package transfer

import (
	"archive/zip"
	"bytes"
	"crypto/md5" //nolint:gosec // matches the sender's choice of hash, not a security decision
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path"

	"github.com/cespare/xxhash"
	"github.com/schollz/croc/v9/src/utils"
)

// confinedDest is every filesystem operation the receiver is allowed to make.
// All of them go through an *os.Root opened on the destination, so a path that
// leaves it fails at the syscall rather than relying on the caller having
// checked. croc.go holds no path-based os.* call on the receive side.
//
// Relative paths here are the slash-separated, already-validated ones that
// validateManifest returned. "" means the destination directory itself.
type confinedDest struct {
	root *os.Root
}

const (
	// received directories and files keep croc's original modes. the sender's
	// FileInfo.Mode is deliberately not honoured: it is peer-controlled, and
	// upstream never applied it either.
	receivedDirPerm  = 0o755
	receivedFilePerm = 0o666
)

// openDest opens dir as the only place this transfer may write. An empty dir
// means the process working directory, which is where receive has always
// written.
func openDest(dir string) (*confinedDest, error) {
	if dir == "" {
		dir = "."
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &confinedDest{root: root}, nil
}

func (d *confinedDest) Close() error { return d.root.Close() }

// mkdirAll creates rel and any missing parent inside the destination.
func (d *confinedDest) mkdirAll(rel string) error {
	if rel == "" || rel == "." {
		return nil
	}
	return d.root.MkdirAll(rel, receivedDirPerm)
}

// lstat is the three-way look: a pre-existing symlink out of the destination is
// an error from os.Root, not an absence, and the difference matters.
func (d *confinedDest) lstat(rel string) (fs.FileInfo, existence, error) {
	return lstatThreeWay(d.root, relOrDot(rel))
}

func (d *confinedDest) isEmptyDir(rel string) (bool, error) {
	dir, err := d.root.Open(relOrDot(rel))
	if err != nil {
		return false, err
	}
	defer dir.Close()

	names, err := dir.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return len(names) == 0, nil
}

// openForWrite opens rel for writing, reporting whether it already existed.
// The two cases are kept apart because the caller resumes into an existing
// file and truncates a new one, which is what upstream did by trying O_WRONLY
// first and falling back to Create.
func (d *confinedDest) openForWrite(rel string) (*os.File, bool, error) {
	f, err := d.root.OpenFile(rel, os.O_WRONLY, receivedFilePerm)
	if err == nil {
		return f, true, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		// a path that escapes, or a directory in the way. do not paper over it
		// by creating something: the manifest was validated, so this is news.
		return nil, false, err
	}
	f, err = d.root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, receivedFilePerm)
	if err != nil {
		return nil, false, err
	}
	return f, false, nil
}

func (d *confinedDest) createEmpty(rel string) error {
	f, err := d.root.Create(rel)
	if err != nil {
		return err
	}
	return f.Close()
}

// symlink replaces rel with a link to target. The target was checked by
// validateManifest: os.Root would create an escaping link quite happily.
func (d *confinedDest) symlink(target, rel string) error {
	if _, ex, err := d.lstat(rel); err == nil && ex == present {
		if err := d.root.Remove(rel); err != nil {
			return err
		}
	}
	return d.root.Symlink(target, rel)
}

func (d *confinedDest) remove(rel string) error { return d.root.Remove(rel) }

func (d *confinedDest) readFile(rel string) ([]byte, error) { return d.root.ReadFile(rel) }

// createTemp makes a uniquely named file in the destination and returns its
// name. It replaces utils.RandomFileName for the stdin case, where the peer
// decides that a name is needed but must not choose it.
func (d *confinedDest) createTemp(prefix string) (string, error) {
	for range 100 {
		name := prefix + randomSuffix()
		f, err := d.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, receivedFilePerm)
		if err == nil {
			f.Close()
			return name, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("could not find an unused name for %s* in the destination", prefix)
}

// hashFile mirrors utils.HashFile over a root-opened handle. The bytes have to
// match the sender's, which hashed with utils.HashFile, or every skip and
// resume decision degrades into re-downloading.
func (d *confinedDest) hashFile(rel, algorithm string) ([]byte, error) {
	info, ex, err := d.lstat(rel)
	if err != nil {
		return nil, err
	}
	if ex == absent {
		return nil, fs.ErrNotExist
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := d.root.Readlink(rel)
		if err != nil {
			return nil, err
		}
		return []byte(utils.SHA256(target)), nil
	}

	var h io.Writer
	var sum func() []byte
	switch algorithm {
	case "", "xxhash":
		x := xxhash.New()
		h, sum = x, func() []byte { return x.Sum(nil) }
	case "md5":
		m := md5.New() //nolint:gosec // the sender picked this, it is not a security control
		h, sum = m, func() []byte { return m.Sum(nil) }
	default:
		// imohash is the remaining one the peer can ask for. it exposes no
		// streaming or ReaderAt api, so computing it here would mean reading a
		// whole received file into memory. returning an error instead costs a
		// re-transfer of a file that might have been skipped, which is the same
		// thing every other hash failure already does.
		return nil, fmt.Errorf("hash algorithm %q cannot be computed on the receive path", algorithm)
	}

	f, err := d.root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return sum(), nil
}

// missingChunks is a port of utils.MissingChunks over a root-opened handle. It
// reports the all-zero regions of a partially received file, which is how a
// resumed transfer knows what to ask for again. Behaviour is deliberately
// identical, including returning nothing when the size does not match.
func (d *confinedDest) missingChunks(rel string, size int64, chunkSize int) (chunkRanges []int64) {
	f, err := d.root.Open(rel)
	if err != nil {
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil || stat.Size() != size {
		return
	}

	emptyBuffer := make([]byte, chunkSize)
	chunkNum := 0
	chunks := make([]int64, int64(math.Ceil(float64(size)/float64(chunkSize))))
	var currentLocation int64
	for {
		buffer := make([]byte, chunkSize)
		bytesread, err := f.Read(buffer)
		if err != nil {
			break
		}
		if bytes.Equal(buffer[:bytesread], emptyBuffer[:bytesread]) {
			chunks[chunkNum] = currentLocation
			chunkNum++
		}
		currentLocation += int64(bytesread)
	}
	if chunkNum == 0 {
		return []int64{}
	}
	chunks = chunks[:chunkNum]
	chunkRanges = []int64{int64(chunkSize), chunks[0]}
	curCount := 0
	for i, chunk := range chunks {
		if i == 0 {
			continue
		}
		curCount++
		if chunk-chunks[i-1] > int64(chunkSize) {
			chunkRanges = append(chunkRanges, int64(curCount))
			chunkRanges = append(chunkRanges, chunk)
			curCount = 0
		}
	}
	return append(chunkRanges, int64(curCount+1))
}

// extractZip replaces utils.UnzipDirectory for the TempFile case, which is how
// `send <folder>` arrives: the sender zips the folder and the receiver unpacks
// it in place.
//
// Upstream's version cannot be used. It guards with
// strings.Contains(path, "..") after a Clean, which rejects a legitimate
// "a..b" and misses a symlink-mode entry (written out as a regular file holding
// the target text); it prompts on stdin mid-extraction; and it calls
// log.Fatalln on any error, taking the process down rather than returning.
//
// Every entry is validated before the first one is written, so a hostile
// archive cannot get half of itself onto disk.
func (d *confinedDest) extractZip(zipRel string) error {
	f, err := d.root.Open(zipRel)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	archive, err := zip.NewReader(f, info.Size())
	if err != nil {
		return err
	}

	paths, err := validateArchive(d.root, archive.File)
	if err != nil {
		return err
	}

	for i, entry := range archive.File {
		rel := paths[i]
		if entry.FileInfo().IsDir() {
			if err := d.mkdirAll(rel); err != nil {
				return err
			}
			continue
		}
		if err := d.mkdirAll(path.Dir(rel)); err != nil {
			return err
		}
		if err := d.writeZipEntry(entry, rel); err != nil {
			return fmt.Errorf("extracting %s: %w", entry.Name, err)
		}
	}
	return nil
}

func (d *confinedDest) writeZipEntry(entry *zip.File, rel string) (err error) {
	perm := entry.Mode().Perm()
	if perm == 0 {
		perm = 0o644
	}

	src, err := entry.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	// receive is non-interactive (NoPrompt and Overwrite are hardcoded), so an
	// existing file is truncated rather than prompted about, which is what the
	// rest of the receive path does.
	dst, err := d.root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := dst.Close(); err == nil {
			err = closeErr
		}
	}()

	_, err = io.Copy(dst, src)
	return err
}

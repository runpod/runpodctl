package transfer

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Everything in this file runs on the receiver before a single byte is written.
//
// os.Root confines writes to the destination, but it is not sufficient on its
// own for two reasons, both measured against the go 1.26 pin rather than
// assumed:
//
//   - root.Symlink("../outside/x", "esc") *succeeds*. Root refuses to follow
//     that link, so nothing escapes at write time, but the receive still leaves
//     an attacker-chosen link pointing out of the tree for the next tool that
//     walks it (a build script, a backup, cp -L) to follow. Only prevalidating
//     the target stops that.
//   - root.Stat on a pre-existing `dir -> /outside` returns "path escapes from
//     parent", *not* ENOENT. Any not-exist branch therefore needs three ways,
//     and falling back to a plain os.Stat in the error arm reopens the hole.
//
// Receive is also all-or-nothing: the peer's manifest is positional and the
// post-transfer loops index it, so a refusal has to happen before the first
// write rather than partway through.

type entryKind uint8

const (
	kindFile entryKind = iota
	kindDir
	kindSymlink
)

func (k entryKind) String() string {
	switch k {
	case kindDir:
		return "folder"
	case kindSymlink:
		return "symlink"
	default:
		return "file"
	}
}

type entrySource uint8

const (
	sourceFile    entrySource = iota // index into SenderInfo.FilesToTransfer
	sourceFolder                     // index into SenderInfo.EmptyFoldersToTransfer
	sourceArchive                    // index into an archive listing
)

// entry is the validator's neutral view of one declared destination, built
// from either a FileInfo or a *zip.File so both go through identical rules.
type entry struct {
	source   entrySource
	index    int    // position in the source slice; never renumbered
	declared string // what the peer sent, for error messages
	rel      string // cleaned slash path relative to the destination, "" for the root
	kind     entryKind
	size     int64
	hash     []byte
	target   string // symlink target, as declared
	tempFile bool
}

// refusalError is every rejection in this file. The reason is reported locally
// and sent to the peer, so it stays lowercase and concise like the rest of the
// cli's output.
type refusalError struct{ reason string }

func (e *refusalError) Error() string { return e.reason }

func refusef(format string, args ...any) error {
	return &refusalError{reason: fmt.Sprintf(format, args...)}
}

// hashAlgorithms are the ones the receiver can compute over a root-opened
// handle. Anything else would send updateIfRecipientHasFileInfo down a path
// that reopens files by name.
var hashAlgorithms = map[string]bool{"": true, "xxhash": true, "md5": true, "imohash": true}

// normalizeRel turns one peer-declared path into a slash-separated path
// relative to the destination, or refuses it.
func normalizeRel(declared string) (string, error) {
	if strings.ContainsRune(declared, 0) {
		return "", refusef("path %q contains a nul byte", declared)
	}
	if strings.ContainsRune(declared, '\\') {
		// a legitimate sender always emits '/' (GetFilesInfo rewrites the
		// separator), and on windows a backslash *is* a separator, so this is
		// refused on every platform rather than branching on GOOS.
		return "", refusef("path %q contains a backslash", declared)
	}
	// checked on the raw string, before splitting, so "/etc/passwd" is refused
	// rather than quietly relocated to "etc/passwd".
	if strings.HasPrefix(declared, "/") || filepath.IsAbs(declared) || filepath.VolumeName(declared) != "" {
		return "", refusef("path %q is absolute", declared)
	}

	var parts []string
	for _, part := range strings.Split(declared, "/") {
		switch part {
		case "", ".":
			continue
		case "..":
			// every ".." component is refused rather than resolved: joining
			// cleans lexically, which is only sound when no intermediate
			// component is a symlink, and the peer declares those components.
			// "file..txt" and "..hidden" are unaffected, being whole names.
			return "", refusef("path %q traverses outside the destination", declared)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "/"), nil
}

// joinRel joins a normalized folder and name, either of which may be empty.
func joinRel(folder, name string) string {
	if folder == "" {
		return name
	}
	if name == "" {
		return folder
	}
	return folder + "/" + name
}

// ancestors lists every directory prefix of rel, shallowest first, excluding
// rel itself and the destination root.
func ancestors(rel string) []string {
	parts := strings.Split(rel, "/")
	if len(parts) < 2 {
		return nil
	}
	out := make([]string, 0, len(parts)-1)
	for i := 1; i < len(parts); i++ {
		out = append(out, strings.Join(parts[:i], "/"))
	}
	return out
}

// manifestEntries converts the peer's manifest into entries in wire order,
// applying every rule that needs only one entry.
func manifestEntries(files, emptyFolders []FileInfo) ([]entry, error) {
	entries := make([]entry, 0, len(files)+len(emptyFolders))

	for i, f := range files {
		folder, err := normalizeRel(f.FolderRemote)
		if err != nil {
			return nil, err
		}
		name, err := normalizeRel(f.Name)
		if err != nil {
			return nil, err
		}
		// Name is a single component for every sender: GetFilesInfo fills it
		// from stat.Name() or info.Name() and puts the directory in
		// FolderRemote. A name carrying separators is the peer trying to reach
		// somewhere FolderRemote was already checked for.
		if name == "" || strings.Contains(name, "/") {
			return nil, refusef("file %d declares name %q, want a single path component", i, f.Name)
		}
		if f.Size < 0 {
			// Truncate(-1) fails, but only after os.Create has already made the
			// file, which would break all-or-nothing.
			return nil, refusef("file %q declares a negative size (%d)", f.Name, f.Size)
		}
		if f.TempFile && f.Symlink != "" {
			return nil, refusef("file %q is declared as both an archive and a symlink", f.Name)
		}
		if f.TempFile && f.Size == 0 {
			// an empty archive cannot be extracted, and the failure would land
			// after the file exists.
			return nil, refusef("archive %q declares a zero size", f.Name)
		}

		kind := kindFile
		if f.Symlink != "" {
			kind = kindSymlink
		}
		entries = append(entries, entry{
			source:   sourceFile,
			index:    i,
			declared: path.Join(f.FolderRemote, f.Name),
			rel:      joinRel(folder, name),
			kind:     kind,
			size:     f.Size,
			hash:     f.Hash,
			target:   f.Symlink,
			tempFile: f.TempFile,
		})
	}

	for i, f := range emptyFolders {
		rel, err := normalizeRel(f.FolderRemote)
		if err != nil {
			return nil, err
		}
		if rel == "" {
			return nil, refusef("empty folder %d declares no path", i)
		}
		entries = append(entries, entry{
			source:   sourceFolder,
			index:    i,
			declared: f.FolderRemote,
			rel:      rel,
			kind:     kindDir,
		})
	}

	return entries, nil
}

// zipEntries converts an archive listing into entries, so extraction obeys the
// same rules as the manifest instead of whichever check happens to be laxer.
func zipEntries(files []*zip.File) ([]entry, error) {
	entries := make([]entry, 0, len(files))
	for i, f := range files {
		rel, err := normalizeRel(f.Name)
		if err != nil {
			return nil, err
		}
		if rel == "" {
			// an entry naming the destination itself.
			return nil, refusef("archive entry %d declares no path", i)
		}

		mode := f.Mode()
		kind := kindFile
		switch {
		case f.FileInfo().IsDir():
			kind = kindDir
		case mode&fs.ModeSymlink != 0:
			// upstream's extractor writes these out as regular files holding
			// the target text, which is neither what the archive asked for nor
			// something to guess at.
			return nil, refusef("archive entry %q is a symlink", f.Name)
		case !mode.IsRegular():
			return nil, refusef("archive entry %q is not a regular file (%s)", f.Name, mode)
		}

		// the uncompressed size is a header field the archive controls; it is
		// used only for the duplicate-content comparison below, never to size
		// an allocation.
		var hash [4]byte
		binary.BigEndian.PutUint32(hash[:], f.CRC32)
		entries = append(entries, entry{
			source:   sourceArchive,
			index:    i,
			declared: f.Name,
			rel:      rel,
			kind:     kind,
			size:     int64(f.UncompressedSize64),
			hash:     hash[:],
		})
	}
	return entries, nil
}

// existence is the middle state of the three-way look every on-disk check
// needs: os.Root reports a pre-existing escaping symlink as an error rather
// than as absent, so "not present" and "not answerable" are different answers.
type existence uint8

const (
	absent existence = iota
	present
)

// lstatThreeWay inspects rel itself, without following a final symlink.
func lstatThreeWay(root *os.Root, rel string) (fs.FileInfo, existence, error) {
	info, err := root.Lstat(rel)
	switch {
	case err == nil:
		return info, present, nil
	case errors.Is(err, fs.ErrNotExist):
		return nil, absent, nil
	default:
		return nil, absent, err
	}
}

// statThreeWay resolves rel, so an escaping or dangling symlink is reported
// rather than silently treated as missing.
func statThreeWay(root *os.Root, rel string) (fs.FileInfo, existence, error) {
	info, err := root.Stat(rel)
	switch {
	case err == nil:
		return info, present, nil
	case errors.Is(err, fs.ErrNotExist):
		return nil, absent, nil
	default:
		return nil, absent, err
	}
}

// foldProbe answers whether a directory inside the destination aliases names
// case-insensitively, by asking the filesystem rather than by reading GOOS.
// The answer is per directory because ext4 carries the casefold attribute that
// way, and it is cached because a manifest can collide many times over.
type foldProbe struct {
	root  *os.Root
	cache map[string]bool
}

func newFoldProbe(root *os.Root) *foldProbe {
	return &foldProbe{root: root, cache: map[string]bool{}}
}

// insensitive reports whether dirRel, or its nearest existing ancestor, folds
// case. dirRel is "" for the destination itself.
func (p *foldProbe) insensitive(dirRel string) (bool, error) {
	probeDir, err := p.nearestExisting(dirRel)
	if err != nil {
		return false, err
	}
	if cached, ok := p.cache[probeDir]; ok {
		return cached, nil
	}
	result, err := p.measure(probeDir)
	if err != nil {
		return false, err
	}
	p.cache[probeDir] = result
	return result, nil
}

// nearestExisting walks up from dirRel to the first directory that is there.
func (p *foldProbe) nearestExisting(dirRel string) (string, error) {
	for current := dirRel; ; current = parentRel(current) {
		info, ex, err := statThreeWay(p.root, relOrDot(current))
		if err != nil {
			return "", refusef("cannot inspect %q in the destination: %v", current, err)
		}
		if ex == present && info.IsDir() {
			return current, nil
		}
		if current == "" {
			// the destination itself; openDest already proved it is a directory.
			return "", nil
		}
	}
}

// measure tests one directory. It prefers reading an existing name, and only
// falls back to writing a probe when the directory offers nothing to test
// with. The probe name is generated here, never peer-supplied.
func (p *foldProbe) measure(dirRel string) (bool, error) {
	dir, err := p.root.Open(relOrDot(dirRel))
	if err != nil {
		return false, refusef("cannot read %q in the destination: %v", dirRel, err)
	}
	names, readErr := dir.Readdirnames(-1)
	dir.Close()
	if readErr != nil {
		return false, refusef("cannot list %q in the destination: %v", dirRel, readErr)
	}

	for _, name := range names {
		swapped, ok := swapCase(name)
		if !ok {
			continue
		}
		original, err := p.root.Lstat(joinRel(dirRel, name))
		if err != nil {
			continue
		}
		other, err := p.root.Lstat(joinRel(dirRel, swapped))
		if err != nil {
			// the swapped name does not resolve, so names are distinct here.
			return false, nil
		}
		return os.SameFile(original, other), nil
	}

	return p.measureByProbe(dirRel)
}

// measureByProbe is the last resort for a directory with nothing readable to
// compare, e.g. an empty one. It is the only write the validator can make, and
// it is undone immediately.
func (p *foldProbe) measureByProbe(dirRel string) (bool, error) {
	name := ".runpodctl-casefold-" + randomSuffix()
	rel := joinRel(dirRel, name)
	f, err := p.root.OpenFile(rel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		// a destination we cannot write is not a destination we can receive
		// into, but say so as a refusal rather than crashing later.
		return false, refusef("cannot write to %q in the destination: %v", dirRel, err)
	}
	f.Close()
	defer p.root.Remove(rel) //nolint:errcheck // best effort cleanup of our own probe

	swapped, ok := swapCase(name)
	if !ok {
		return false, nil
	}
	original, err := p.root.Lstat(rel)
	if err != nil {
		return false, refusef("cannot inspect %q in the destination: %v", dirRel, err)
	}
	other, err := p.root.Lstat(joinRel(dirRel, swapped))
	if err != nil {
		return false, nil
	}
	return os.SameFile(original, other), nil
}

// swapCase inverts the case of every cased letter, reporting false when there
// was nothing to invert and the name is therefore useless as a probe.
func swapCase(name string) (string, bool) {
	var b strings.Builder
	b.Grow(len(name))
	changed := false
	for _, r := range name {
		switch {
		case unicode.IsLower(r):
			b.WriteRune(unicode.ToUpper(r))
			changed = true
		case unicode.IsUpper(r):
			b.WriteRune(unicode.ToLower(r))
			changed = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String(), changed
}

func parentRel(rel string) string {
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[:i]
	}
	return ""
}

// relOrDot maps the destination root to the name os.Root expects for it.
func relOrDot(rel string) string {
	if rel == "" {
		return "."
	}
	return rel
}

// randomSuffix names the case-folding probe. It is not security material, but
// it must not collide with a peer-declared name, so it comes from crypto/rand.
func randomSuffix() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read does not fail in practice; a timestamp still avoids a
		// collision with a concurrent receive better than a constant would.
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

// validator holds the state the cross-entry rules share.
//
// Keys are compared exactly first. A case-folding filesystem makes distinct
// declared names the same destination, so every lookup that misses exactly
// also consults a lowercase index, and only then asks the filesystem whether
// that directory really folds. Probing lazily matters: receiving into an empty
// directory is the common case, and the probe of last resort has to write.
type validator struct {
	root  *os.Root
	probe *foldProbe

	files    map[string]entry    // exact rel -> file or symlink entry
	symlinks map[string]bool     // exact rel declared as a symlink
	dirs     map[string]bool     // exact rel that must end up a directory
	lowered  map[string][]string // lowercased rel -> exact rels seen
}

func newValidator(root *os.Root) *validator {
	return &validator{
		root:     root,
		probe:    newFoldProbe(root),
		files:    map[string]entry{},
		symlinks: map[string]bool{},
		dirs:     map[string]bool{},
		lowered:  map[string][]string{},
	}
}

// aliases returns the already-seen rels that this rel would share a
// destination with: itself when seen exactly, plus any case variant on a
// filesystem that folds the directory holding it.
func (v *validator) aliases(rel string) ([]string, error) {
	var out []string
	if v.claimed(rel) {
		out = append(out, rel)
	}

	for _, candidate := range v.lowered[strings.ToLower(rel)] {
		if candidate == rel {
			continue
		}
		folds, err := v.probe.insensitive(parentRel(rel))
		if err != nil {
			return nil, err
		}
		if folds {
			out = append(out, candidate)
		}
	}
	return out, nil
}

// claimed reports whether some earlier entry already owns this exact rel.
func (v *validator) claimed(rel string) bool {
	if _, isFile := v.files[rel]; isFile {
		return true
	}
	return v.dirs[rel]
}

func (v *validator) note(rel string) {
	lower := strings.ToLower(rel)
	for _, seen := range v.lowered[lower] {
		if seen == rel {
			return
		}
	}
	v.lowered[lower] = append(v.lowered[lower], rel)
}

// sameContent reports whether two declarations of one destination agree well
// enough to be harmless. `send a.txt a.txt` is a legitimate invocation that
// GetFilesInfo never deduped, and the old receiver overwrote identical content
// without complaint, so it must keep working.
func sameContent(a, b entry) bool {
	return a.kind == b.kind &&
		a.size == b.size &&
		a.target == b.target &&
		a.tempFile == b.tempFile &&
		bytes.Equal(a.hash, b.hash)
}

// checkEntries applies every rule that depends on more than one entry or on
// the destination's current contents.
func checkEntries(root *os.Root, entries []entry) error {
	v := newValidator(root)

	// pass one: claim destinations, and refuse conflicting duplicates.
	for _, e := range entries {
		aliases, err := v.aliases(e.rel)
		if err != nil {
			return err
		}
		for _, alias := range aliases {
			prior, isFile := v.files[alias]
			switch {
			case e.kind == kindDir && isFile:
				return refusef("%q is declared both as a directory and as a file", e.declared)
			case e.kind == kindDir:
				// another declaration of the same directory, or a directory
				// that an earlier entry already needs to exist.
				continue
			case !isFile:
				return refusef("%q is declared both as a file and as a directory", e.declared)
			case sameContent(prior, e):
				// a harmless repeat. `send a.txt a.txt` produces exactly this:
				// GetFilesInfo never deduped, and the old receiver overwrote
				// identical content without complaint, so it keeps working.
				continue
			case alias == e.rel:
				return refusef("%q is declared twice with different contents", e.declared)
			default:
				return refusef("%q and %q are the same destination on this filesystem but declare different contents", prior.declared, e.declared)
			}
		}

		v.note(e.rel)
		switch e.kind {
		case kindDir:
			v.dirs[e.rel] = true
		case kindSymlink:
			v.symlinks[e.rel] = true
			v.files[e.rel] = e
		default:
			v.files[e.rel] = e
		}

		// every ancestor has to be a directory, so a declaration of the
		// ancestor itself as a file is a conflict.
		for _, dir := range ancestors(e.rel) {
			v.note(dir)
			v.dirs[dir] = true
		}
	}

	// pass two: a path cannot be both a file and the prefix of another path.
	// checked after every claim is in, so manifest order does not matter, and
	// through aliases so a case variant cannot be the directory ("A" as a file
	// plus "a/b" is one name doing both jobs on a folding filesystem).
	// iterating entries rather than the map keeps the reported conflict stable.
	for _, e := range entries {
		if e.kind == kindDir {
			continue
		}
		aliases, err := v.aliases(e.rel)
		if err != nil {
			return err
		}
		for _, alias := range aliases {
			if v.dirs[alias] {
				return refusef("%q is declared as a %s and also as a directory containing other entries", e.declared, e.kind)
			}
		}
	}

	// pass three: nothing may be written through a symlink this transfer
	// declares, in either order, and no declared symlink may point out.
	for _, e := range entries {
		for _, dir := range ancestors(e.rel) {
			aliases, err := v.aliases(dir)
			if err != nil {
				return err
			}
			for _, alias := range aliases {
				if v.symlinks[alias] {
					return refusef("%q would be written through %q, which this transfer declares as a symlink", e.declared, alias)
				}
			}
		}
		if e.kind == kindSymlink {
			if err := v.checkSymlinkTarget(e); err != nil {
				return err
			}
		}
	}

	// pass four: what is already on disk. every ancestor that exists must be a
	// real directory we can descend, and a pre-existing escaping symlink is
	// reported by os.Root as an error rather than as absent.
	checked := map[string]bool{}
	for _, e := range entries {
		for _, dir := range ancestors(e.rel) {
			if checked[dir] {
				continue
			}
			checked[dir] = true
			if err := v.checkExistingDir(dir); err != nil {
				return err
			}
		}
	}

	return nil
}

// checkExistingDir refuses an ancestor that cannot hold the transfer.
func (v *validator) checkExistingDir(rel string) error {
	info, ex, err := lstatThreeWay(v.root, rel)
	if err != nil {
		return refusef("cannot inspect %q in the destination: %v", rel, err)
	}
	if ex == absent {
		return nil // MkdirAll will create it
	}

	if info.Mode()&fs.ModeSymlink == 0 {
		if !info.IsDir() {
			return refusef("%q already exists as a file, but the transfer needs it to be a directory", rel)
		}
		return nil
	}

	// a symlink: it may point inside the destination, in which case os.Root
	// follows it happily, or outside, in which case Root reports "escapes from
	// parent" rather than ENOENT. resolving is the only way to tell.
	target, ex, err := statThreeWay(v.root, rel)
	if err != nil {
		return refusef("%q in the destination is a symlink pointing outside it (%v)", rel, err)
	}
	if ex == absent {
		return refusef("%q in the destination is a symlink with no target", rel)
	}
	if !target.IsDir() {
		return refusef("%q in the destination is a symlink to a file, but the transfer needs a directory", rel)
	}
	return nil
}

// checkSymlinkTarget refuses a declared symlink whose target leaves the
// destination.
//
// os.Root will not follow such a link, so nothing is written outside during
// the transfer, but it will happily *create* it, and the link then outlives
// the receive for the next thing that walks the tree. Resolution is lexical,
// which only holds while no directory it leaves is itself a symlink, so any
// symlink met on the way is refused rather than guessed at.
func (v *validator) checkSymlinkTarget(e entry) error {
	target := e.target
	if strings.ContainsRune(target, 0) {
		return refusef("symlink %q has a target containing a nul byte", e.declared)
	}
	if strings.ContainsRune(target, '\\') {
		return refusef("symlink %q has a target containing a backslash (%q)", e.declared, target)
	}
	if strings.HasPrefix(target, "/") || filepath.IsAbs(target) || filepath.VolumeName(target) != "" {
		return refusef("symlink %q points to the absolute path %q", e.declared, target)
	}

	var stack []string
	if parent := parentRel(e.rel); parent != "" {
		stack = strings.Split(parent, "/")
	}

	for _, part := range strings.Split(target, "/") {
		switch part {
		case "", ".":
			continue
		case "..":
			if len(stack) == 0 {
				return refusef("symlink %q points outside the destination (target %q)", e.declared, target)
			}
			// leaving a directory is only the same as removing a component
			// when that directory is not a symlink.
			if err := v.requireRealDir(strings.Join(stack, "/")); err != nil {
				return fmt.Errorf("symlink %q target %q: %w", e.declared, target, err)
			}
			stack = stack[:len(stack)-1]
		default:
			stack = append(stack, part)
		}
	}
	return nil
}

// requireRealDir refuses a path that a lexical ".." cannot be reasoned about.
func (v *validator) requireRealDir(rel string) error {
	if rel == "" {
		return nil // the destination itself
	}

	aliases, err := v.aliases(rel)
	if err != nil {
		return err
	}
	for _, alias := range aliases {
		if v.symlinks[alias] {
			return refusef("resolution crosses %q, which this transfer declares as a symlink", alias)
		}
	}

	info, ex, err := lstatThreeWay(v.root, rel)
	if err != nil {
		return refusef("cannot inspect %q in the destination: %v", rel, err)
	}
	if ex == present && info.Mode()&fs.ModeSymlink != 0 {
		return refusef("resolution crosses %q, which already exists as a symlink", rel)
	}
	return nil
}

// validateManifest is the receiver's single gate. It returns the destination
// for every declared file and every declared empty folder, positionally, so
// nothing downstream re-derives a path from FolderRemote and Name.
func validateManifest(root *os.Root, info SenderInfo) (filePaths, folderPaths []string, err error) {
	if !hashAlgorithms[info.HashAlgorithm] {
		return nil, nil, refusef("unsupported hash algorithm %q", info.HashAlgorithm)
	}
	if info.TotalNumberFolders < 0 {
		return nil, nil, refusef("negative folder count (%d)", info.TotalNumberFolders)
	}

	entries, err := manifestEntries(info.FilesToTransfer, info.EmptyFoldersToTransfer)
	if err != nil {
		return nil, nil, err
	}
	if err := checkEntries(root, entries); err != nil {
		return nil, nil, err
	}

	filePaths = make([]string, len(info.FilesToTransfer))
	folderPaths = make([]string, len(info.EmptyFoldersToTransfer))
	for _, e := range entries {
		switch e.source {
		case sourceFile:
			filePaths[e.index] = e.rel
		case sourceFolder:
			folderPaths[e.index] = e.rel
		}
	}
	return filePaths, folderPaths, nil
}

// validateArchive is the same gate for a zip the peer sent as a TempFile.
func validateArchive(root *os.Root, files []*zip.File) ([]string, error) {
	entries, err := zipEntries(files)
	if err != nil {
		return nil, err
	}
	if err := checkEntries(root, entries); err != nil {
		return nil, err
	}
	paths := make([]string, len(files))
	for _, e := range entries {
		paths[e.index] = e.rel
	}
	return paths, nil
}

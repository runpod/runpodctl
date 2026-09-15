package transfer

// This file contains the croc implementation adapted from github.com/schollz/croc/v9
// Most of the core croc functionality is imported directly from the library.
// This wrapper provides runpod-specific relay selection and configuration.

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/denisbrodbeck/machineid"
	log "github.com/schollz/logger"
	"github.com/schollz/pake/v3"
	"github.com/schollz/peerdiscovery"
	"github.com/schollz/progressbar/v3"

	"github.com/schollz/croc/v9/src/comm"
	"github.com/schollz/croc/v9/src/compress"
	"github.com/schollz/croc/v9/src/crypt"
	"github.com/schollz/croc/v9/src/message"
	"github.com/schollz/croc/v9/src/models"
	"github.com/schollz/croc/v9/src/tcp"
	"github.com/schollz/croc/v9/src/utils"
)

var (
	ipRequest        = []byte("ips?")
	handshakeRequest = []byte("handshake")
)

func init() {
	log.SetLevel("warn")
}

// Options specifies user specific options
type Options struct {
	IsSender       bool
	SharedSecret   string
	Debug          bool
	RelayAddress   string
	RelayAddress6  string
	RelayPorts     []string
	RelayPassword  string
	Stdout         bool
	NoPrompt       bool
	NoMultiplexing bool
	DisableLocal   bool
	OnlyLocal      bool
	IgnoreStdin    bool
	Ask            bool
	SendingText    bool
	NoCompress     bool
	IP             string
	Overwrite      bool
	Curve          string
	HashAlgorithm  string
	ThrottleUpload string
	ZipFolder      bool
	// Destination is where a receive may write. Empty means the process
	// working directory, which is where receive has always written. It exists
	// so a test can point a receiver somewhere other than its own cwd, which
	// is process-global; there is no flag for it.
	Destination string
}

// Client holds the state of the croc transfer
type Client struct {
	Options                         Options
	Pake                            *pake.Pake
	Key                             []byte
	ExternalIP, ExternalIPConnected string

	Step1ChannelSecured       bool
	Step2FileInfoTransferred  bool
	Step3RecipientRequestFile bool
	Step4FileTransferred      bool
	Step5CloseChannels        bool
	SuccessfulTransfer        bool

	FilesToTransfer           []FileInfo
	EmptyFoldersToTransfer    []FileInfo
	TotalNumberOfContents     int
	TotalNumberFolders        int
	FilesToTransferCurrentNum int
	FilesHasFinished          map[int]struct{}

	// dest is the only filesystem surface the receiver writes through.
	dest *confinedDest
	// filePaths and folderPaths are the validated destinations for
	// FilesToTransfer and EmptyFoldersToTransfer, positionally. The wire
	// protocol indexes by position, so these are never renumbered and nothing
	// downstream re-derives a path from FolderRemote and Name.
	filePaths   []string
	folderPaths []string
	// refused records a rejected manifest. It is checked before the peer's
	// SuccessfulTransfer flag can clear the error, because that flag is
	// peer-controlled and a refusal it can launder is not a refusal.
	refused error

	CurrentFile            *os.File
	CurrentFileChunkRanges []int64
	CurrentFileChunks      []int64
	CurrentFileIsClosed    bool
	LastFolder             string

	TotalSent              int64
	TotalChunksTransferred int
	chunkMap               map[uint64]struct{}
	limiter                *rate.Limiter

	conn []*comm.Comm

	bar             *progressbar.ProgressBar
	longestFilename int
	firstSend       bool

	mutex                    *sync.Mutex
	fread                    *os.File
	numfinished              int
	quit                     chan bool
	finishedNum              int
	numberOfTransferredFiles int
}

// FileInfo registers the information about the file
type FileInfo struct {
	Name         string      `json:"n,omitempty"`
	FolderRemote string      `json:"fr,omitempty"`
	FolderSource string      `json:"fs,omitempty"`
	Hash         []byte      `json:"h,omitempty"`
	Size         int64       `json:"s,omitempty"`
	ModTime      time.Time   `json:"m,omitempty"`
	IsCompressed bool        `json:"c,omitempty"`
	IsEncrypted  bool        `json:"e,omitempty"`
	Symlink      string      `json:"sy,omitempty"`
	Mode         os.FileMode `json:"md,omitempty"`
	TempFile     bool        `json:"tf,omitempty"`
}

// RemoteFileRequest requests specific bytes
type RemoteFileRequest struct {
	CurrentFileChunkRanges    []int64
	FilesToTransferCurrentNum int
	MachineID                 string
}

// SenderInfo lists the files to be transferred
type SenderInfo struct {
	FilesToTransfer        []FileInfo
	EmptyFoldersToTransfer []FileInfo
	TotalNumberFolders     int
	MachineID              string
	Ask                    bool
	SendingText            bool
	NoCompress             bool
	HashAlgorithm          string
}

// New establishes a new connection for transferring files
func New(ops Options) (c *Client, err error) {
	c = new(Client)
	c.FilesHasFinished = make(map[int]struct{})
	c.Options = ops

	if c.Options.Debug {
		log.SetLevel("debug")
	} else {
		log.SetLevel("warn")
	}

	if len(c.Options.SharedSecret) < 6 {
		err = fmt.Errorf("code is too short")
		return
	}

	c.conn = make([]*comm.Comm, 16)

	if len(c.Options.ThrottleUpload) > 1 && c.Options.IsSender {
		upload := c.Options.ThrottleUpload[:len(c.Options.ThrottleUpload)-1]
		uploadLimit, err := strconv.ParseInt(upload, 10, 64)
		if err != nil {
			panic("could not parse given upload limit")
		}
		minBurstSize := models.TCP_BUFFER_SIZE
		var rt rate.Limit
		switch unit := string(c.Options.ThrottleUpload[len(c.Options.ThrottleUpload)-1:]); unit {
		case "g", "G":
			uploadLimit = uploadLimit * 1024 * 1024 * 1024
		case "m", "M":
			uploadLimit = uploadLimit * 1024 * 1024
		case "k", "K":
			uploadLimit = uploadLimit * 1024
		default:
			uploadLimit, err = strconv.ParseInt(c.Options.ThrottleUpload, 10, 64)
			if err != nil {
				panic("could not parse given upload limit")
			}
		}
		rt = rate.Every(time.Second / (4 * time.Duration(uploadLimit)))
		if int(uploadLimit) > minBurstSize {
			minBurstSize = int(uploadLimit)
		}
		c.limiter = rate.NewLimiter(rt, minBurstSize)
	}

	if !c.Options.IsSender {
		c.Pake, err = pake.InitCurve([]byte(c.Options.SharedSecret[5:]), 0, c.Options.Curve)
	}
	if err != nil {
		return
	}

	c.mutex = &sync.Mutex{}
	return
}

func isEmptyFolder(folderPath string) (bool, error) {
	f, err := os.Open(folderPath)
	if err != nil {
		return false, err
	}
	defer f.Close()

	_, err = f.Readdirnames(1)
	if err == io.EOF {
		return true, nil
	}
	return false, nil
}

// GetFilesInfo retrieves file information for transfer
func GetFilesInfo(fnames []string, zipfolder bool) (filesInfo []FileInfo, emptyFolders []FileInfo, totalNumberFolders int, err error) {
	totalNumberFolders = 0
	var paths []string
	for _, fname := range fnames {
		if strings.Contains(fname, "*") {
			matches, errGlob := filepath.Glob(fname)
			if errGlob != nil {
				err = errGlob
				return
			}
			paths = append(paths, matches...)
			continue
		} else {
			paths = append(paths, fname)
		}
	}

	for _, pathName := range paths {
		stat, errStat := os.Lstat(pathName)
		if errStat != nil {
			err = errStat
			return
		}

		absPath, errAbs := filepath.Abs(pathName)
		if errAbs != nil {
			err = errAbs
			return
		}

		if stat.IsDir() && zipfolder {
			if pathName[len(pathName)-1:] != "/" {
				pathName += "/"
			}
			pathName := filepath.Dir(pathName)
			dest := filepath.Base(pathName) + ".zip"
			utils.ZipDirectory(dest, pathName) //nolint
			stat, errStat = os.Lstat(dest)
			if errStat != nil {
				err = errStat
				return
			}
			absPath, errAbs = filepath.Abs(dest)
			if errAbs != nil {
				err = errAbs
				return
			}
			filesInfo = append(filesInfo, FileInfo{
				Name:         stat.Name(),
				FolderRemote: "./",
				FolderSource: filepath.Dir(absPath),
				Size:         stat.Size(),
				ModTime:      stat.ModTime(),
				Mode:         stat.Mode(),
				TempFile:     true,
			})
			continue
		}

		if stat.IsDir() {
			err = filepath.Walk(absPath,
				func(walkPath string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}
					remoteFolder := strings.TrimPrefix(filepath.Dir(walkPath),
						filepath.Dir(absPath)+string(os.PathSeparator))
					if !info.IsDir() {
						filesInfo = append(filesInfo, FileInfo{
							Name:         info.Name(),
							FolderRemote: strings.Replace(remoteFolder, string(os.PathSeparator), "/", -1) + "/",
							FolderSource: filepath.Dir(walkPath),
							Size:         info.Size(),
							ModTime:      info.ModTime(),
							Mode:         info.Mode(),
							TempFile:     false,
						})
					} else {
						totalNumberFolders++
						isEmpty, _ := isEmptyFolder(walkPath)
						if isEmpty {
							emptyFolders = append(emptyFolders, FileInfo{
								FolderRemote: strings.Replace(strings.TrimPrefix(walkPath,
									filepath.Dir(absPath)+string(os.PathSeparator)), string(os.PathSeparator), "/", -1) + "/",
							})
						}
					}
					return nil
				})
			if err != nil {
				return
			}
		} else {
			filesInfo = append(filesInfo, FileInfo{
				Name:         stat.Name(),
				FolderRemote: "./",
				FolderSource: filepath.Dir(absPath),
				Size:         stat.Size(),
				ModTime:      stat.ModTime(),
				Mode:         stat.Mode(),
				TempFile:     false,
			})
		}
	}
	return
}

func (c *Client) sendCollectFiles(filesInfo []FileInfo) (err error) {
	c.FilesToTransfer = filesInfo
	totalFilesSize := int64(0)

	for i, fileInfo := range c.FilesToTransfer {
		var fullPath string
		fullPath = fileInfo.FolderSource + string(os.PathSeparator) + fileInfo.Name
		fullPath = filepath.Clean(fullPath)

		if len(fileInfo.Name) > c.longestFilename {
			c.longestFilename = len(fileInfo.Name)
		}

		if fileInfo.Mode&os.ModeSymlink != 0 {
			c.FilesToTransfer[i].Symlink, err = os.Readlink(fullPath)
			if err != nil {
				log.Debugf("error getting symlink: %s", err.Error())
			}
		}

		if c.Options.HashAlgorithm == "" {
			c.Options.HashAlgorithm = "xxhash"
		}

		c.FilesToTransfer[i].Hash, err = utils.HashFile(fullPath, c.Options.HashAlgorithm)
		totalFilesSize += fileInfo.Size
		if err != nil {
			return
		}
		fmt.Fprintf(os.Stderr, "\r                                 ")
		fmt.Fprintf(os.Stderr, "\rsending %d files (%s)", i, utils.ByteCountDecimal(totalFilesSize))
	}
	fname := fmt.Sprintf("%d files", len(c.FilesToTransfer))
	folderName := fmt.Sprintf("%d folders", c.TotalNumberFolders)
	if len(c.FilesToTransfer) == 1 {
		fname = fmt.Sprintf("'%s'", c.FilesToTransfer[0].Name)
	}
	if strings.HasPrefix(fname, "'croc-stdin-") {
		fname = "'stdin'"
		if c.Options.SendingText {
			fname = "'text'"
		}
	}

	fmt.Fprintf(os.Stderr, "\r                                 ")
	if c.TotalNumberFolders > 0 {
		fmt.Fprintf(os.Stderr, "\rsending %s and %s (%s)\n", fname, folderName, utils.ByteCountDecimal(totalFilesSize))
	} else {
		fmt.Fprintf(os.Stderr, "\rsending %s (%s)\n", fname, utils.ByteCountDecimal(totalFilesSize))
	}
	return
}

func (c *Client) setupLocalRelay() {
	firstPort, _ := strconv.Atoi(c.Options.RelayPorts[0])
	openPorts := utils.FindOpenPorts("localhost", firstPort, len(c.Options.RelayPorts))
	if len(openPorts) < len(c.Options.RelayPorts) {
		panic("not enough open ports to run local relay")
	}
	for i, port := range openPorts {
		c.Options.RelayPorts[i] = fmt.Sprint(port)
	}
	for _, port := range c.Options.RelayPorts {
		go func(portStr string) {
			debugString := "warn"
			if c.Options.Debug {
				debugString = "debug"
			}
			err := tcp.Run(debugString, "localhost", portStr, c.Options.RelayPassword, strings.Join(c.Options.RelayPorts[1:], ","))
			if err != nil {
				panic(err)
			}
		}(port)
	}
}

func (c *Client) broadcastOnLocalNetwork(useipv6 bool) {
	var timeLimit time.Duration
	if c.Options.OnlyLocal {
		timeLimit = -1 * time.Second
	} else {
		timeLimit = 30 * time.Second
	}
	settings := peerdiscovery.Settings{
		Limit:     -1,
		Payload:   []byte("croc" + c.Options.RelayPorts[0]),
		Delay:     20 * time.Millisecond,
		TimeLimit: timeLimit,
	}
	if useipv6 {
		settings.IPVersion = peerdiscovery.IPv6
	}

	_, err := peerdiscovery.Discover(settings)
	if err != nil {
		log.Debug(err)
	}
}

func (c *Client) transferOverLocalRelay(errchan chan<- error) {
	time.Sleep(500 * time.Millisecond)
	conn, banner, ipaddr, err := tcp.ConnectToTCPServer("localhost:"+c.Options.RelayPorts[0], c.Options.RelayPassword, c.Options.SharedSecret[:3])
	if err != nil {
		return
	}
	for {
		data, _ := conn.Receive()
		if bytes.Equal(data, handshakeRequest) {
			break
		} else if bytes.Equal(data, []byte{1}) {
			log.Debug("got ping")
		}
	}
	c.conn[0] = conn
	c.Options.RelayAddress = "localhost"
	c.Options.RelayPorts = strings.Split(banner, ",")
	if c.Options.NoMultiplexing {
		c.Options.RelayPorts = []string{c.Options.RelayPorts[0]}
	}
	c.ExternalIP = ipaddr
	errchan <- c.transfer()
}

// Send will send the specified file
func (c *Client) Send(filesInfo []FileInfo, emptyFoldersToTransfer []FileInfo, totalNumberFolders int) (err error) {
	c.EmptyFoldersToTransfer = emptyFoldersToTransfer
	c.TotalNumberFolders = totalNumberFolders
	c.TotalNumberOfContents = len(filesInfo)
	err = c.sendCollectFiles(filesInfo)
	if err != nil {
		return
	}
	flags := &strings.Builder{}
	fmt.Fprintf(os.Stderr, "code is: %[1]s\non the other computer run\n\nrunpodctl receive %[2]s%[1]s\n", c.Options.SharedSecret, flags.String())
	if c.Options.Ask {
		machid, _ := machineid.ID()
		fmt.Fprintf(os.Stderr, "\ryour machine id is '%s'\n", machid)
	}

	errchan := make(chan error, 1)

	if !c.Options.DisableLocal {
		errchan = make(chan error, 2)
		c.setupLocalRelay()
		go c.broadcastOnLocalNetwork(false)
		go c.broadcastOnLocalNetwork(true)
		go c.transferOverLocalRelay(errchan)
	}

	if !c.Options.OnlyLocal {
		go func() {
			var ipaddr, banner string
			var conn *comm.Comm
			durations := []time.Duration{100 * time.Millisecond, 5 * time.Second}
			for i, address := range []string{c.Options.RelayAddress6, c.Options.RelayAddress} {
				if address == "" {
					continue
				}
				host, port, _ := net.SplitHostPort(address)
				if port == "" {
					host = address
					port = models.DEFAULT_PORT
				}
				address = net.JoinHostPort(host, port)
				conn, banner, ipaddr, err = tcp.ConnectToTCPServer(address, c.Options.RelayPassword, c.Options.SharedSecret[:3], durations[i])
				if err == nil {
					c.Options.RelayAddress = address
					break
				}
			}
			if conn == nil && err == nil {
				err = fmt.Errorf("could not connect")
			}
			if err != nil {
				err = fmt.Errorf("could not connect to %s: %w", c.Options.RelayAddress, err)
				errchan <- err
				return
			}
			for {
				data, errConn := conn.Receive()
				if errConn != nil {
					log.Debugf("[%+v] had error: %s", conn, errConn.Error())
				}
				if bytes.Equal(data, ipRequest) {
					var ips []string
					if !c.Options.DisableLocal {
						ips, err = utils.GetLocalIPs()
						if err != nil {
							log.Debugf("error getting local ips: %v", err)
						}
						ips = append([]string{c.Options.RelayPorts[0]}, ips...)
					}
					bips, _ := json.Marshal(ips)
					if err := conn.Send(bips); err != nil {
						log.Errorf("error sending: %v", err)
					}
				} else if bytes.Equal(data, handshakeRequest) {
					break
				} else if bytes.Equal(data, []byte{1}) {
					continue
				} else {
					errchan <- fmt.Errorf("gracefully refusing using the public relay")
					return
				}
			}

			c.conn[0] = conn
			c.Options.RelayPorts = strings.Split(banner, ",")
			if c.Options.NoMultiplexing {
				c.Options.RelayPorts = []string{c.Options.RelayPorts[0]}
			}
			c.ExternalIP = ipaddr
			errchan <- c.transfer()
		}()
	}

	err = <-errchan
	if err == nil {
		return
	} else {
		if strings.Contains(err.Error(), "could not secure channel") {
			return err
		}
	}
	if !c.Options.DisableLocal {
		if strings.Contains(err.Error(), "refusing files") || strings.Contains(err.Error(), "EOF") || strings.Contains(err.Error(), "bad password") {
			errchan <- err
		}
		err = <-errchan
	}
	return err
}

// Receive will receive a file
func (c *Client) Receive() (err error) {
	// opened before connecting: an unusable destination should fail now, not
	// after a relay handshake and a pake exchange.
	c.dest, err = openDest(c.Options.Destination)
	if err != nil {
		return fmt.Errorf("cannot open destination: %w", err)
	}
	defer c.dest.Close()

	fmt.Fprintf(os.Stderr, "connecting...")
	usingLocal := false
	isIPset := false

	if c.Options.OnlyLocal || c.Options.IP != "" {
		c.Options.RelayAddress = ""
		c.Options.RelayAddress6 = ""
	}

	if c.Options.IP != "" {
		if strings.Count(c.Options.IP, ":") >= 2 {
			c.Options.RelayAddress6 = c.Options.IP
		}
		if strings.Contains(c.Options.IP, ".") {
			c.Options.RelayAddress = c.Options.IP
		}
		isIPset = true
	}

	if !c.Options.DisableLocal && !isIPset {
		var discoveries []peerdiscovery.Discovered
		var wgDiscovery sync.WaitGroup
		var dmux sync.Mutex
		wgDiscovery.Add(2)
		go func() {
			defer wgDiscovery.Done()
			ipv4discoveries, err1 := peerdiscovery.Discover(peerdiscovery.Settings{
				Limit:     1,
				Payload:   []byte("ok"),
				Delay:     20 * time.Millisecond,
				TimeLimit: 200 * time.Millisecond,
			})
			if err1 == nil && len(ipv4discoveries) > 0 {
				dmux.Lock()
				err = err1
				discoveries = append(discoveries, ipv4discoveries...)
				dmux.Unlock()
			}
		}()
		go func() {
			defer wgDiscovery.Done()
			ipv6discoveries, err1 := peerdiscovery.Discover(peerdiscovery.Settings{
				Limit:     1,
				Payload:   []byte("ok"),
				Delay:     20 * time.Millisecond,
				TimeLimit: 200 * time.Millisecond,
				IPVersion: peerdiscovery.IPv6,
			})
			if err1 == nil && len(ipv6discoveries) > 0 {
				dmux.Lock()
				err = err1
				discoveries = append(discoveries, ipv6discoveries...)
				dmux.Unlock()
			}
		}()
		wgDiscovery.Wait()

		if err == nil && len(discoveries) > 0 {
			for i := 0; i < len(discoveries); i++ {
				if !bytes.HasPrefix(discoveries[i].Payload, []byte("croc")) {
					continue
				}
				portToUse := string(bytes.TrimPrefix(discoveries[i].Payload, []byte("croc")))
				if portToUse == "" {
					portToUse = models.DEFAULT_PORT
				}
				address := net.JoinHostPort(discoveries[i].Address, portToUse)
				errPing := tcp.PingServer(address)
				if errPing == nil {
					c.Options.RelayAddress = address
					c.ExternalIPConnected = c.Options.RelayAddress
					c.Options.RelayAddress6 = ""
					usingLocal = true
					break
				}
			}
		}
	}
	var banner string
	durations := []time.Duration{200 * time.Millisecond, 5 * time.Second}
	err = fmt.Errorf("found no addresses to connect")
	for i, address := range []string{c.Options.RelayAddress6, c.Options.RelayAddress} {
		if address == "" {
			continue
		}
		var host, port string
		host, port, _ = net.SplitHostPort(address)
		if port == "" {
			host = address
			port = models.DEFAULT_PORT
		}
		address = net.JoinHostPort(host, port)
		c.conn[0], banner, c.ExternalIP, err = tcp.ConnectToTCPServer(address, c.Options.RelayPassword, c.Options.SharedSecret[:3], durations[i])
		if err == nil {
			c.Options.RelayAddress = address
			break
		}
	}
	if err != nil {
		err = fmt.Errorf("could not connect to %s: %w", c.Options.RelayAddress, err)
		return
	}

	if !usingLocal && !c.Options.DisableLocal && !isIPset {
		if err := c.conn[0].Send(ipRequest); err != nil {
			log.Errorf("ips send error: %v", err)
		}
		data, errRecv := c.conn[0].Receive()
		if errRecv != nil {
			return errRecv
		}
		var ips []string
		if err := json.Unmarshal(data, &ips); err != nil {
			log.Debugf("ips unmarshal error: %v", err)
		}
		if len(ips) > 1 {
			port := ips[0]
			ips = ips[1:]
			for _, ip := range ips {
				ipv4Addr, ipv4Net, errNet := net.ParseCIDR(fmt.Sprintf("%s/24", ip))
				log.Debugf("ipv4Add4: %+v, ipv4Net: %+v, err: %+v", ipv4Addr, ipv4Net, errNet)
				localIps, _ := utils.GetLocalIPs()
				haveLocalIP := false
				for _, localIP := range localIps {
					localIPparsed := net.ParseIP(localIP)
					if ipv4Net.Contains(localIPparsed) {
						haveLocalIP = true
						break
					}
				}
				if !haveLocalIP {
					continue
				}

				serverTry := net.JoinHostPort(ip, port)
				conn, banner2, externalIP, errConn := tcp.ConnectToTCPServer(serverTry, c.Options.RelayPassword, c.Options.SharedSecret[:3], 500*time.Millisecond)
				if errConn != nil {
					continue
				}
				banner = banner2
				c.Options.RelayAddress = serverTry
				c.ExternalIP = externalIP
				c.conn[0].Close()
				c.conn[0] = nil
				c.conn[0] = conn
				break
			}
		}
	}

	if err := c.conn[0].Send(handshakeRequest); err != nil {
		log.Errorf("handshake send error: %v", err)
	}
	c.Options.RelayPorts = strings.Split(banner, ",")
	if c.Options.NoMultiplexing {
		c.Options.RelayPorts = []string{c.Options.RelayPorts[0]}
	}
	fmt.Fprintf(os.Stderr, "\rsecuring channel...")
	err = c.transfer()
	if err == nil {
		if c.numberOfTransferredFiles+len(c.EmptyFoldersToTransfer) == 0 {
			fmt.Fprintf(os.Stderr, "\rno files transferred.")
		}
	}
	return
}

func (c *Client) transfer() (err error) {
	c.quit = make(chan bool)

	if !c.Options.IsSender && !c.Step1ChannelSecured {
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type:   message.TypePAKE,
			Bytes:  c.Pake.Bytes(),
			Bytes2: []byte(c.Options.Curve),
		})
		if err != nil {
			return
		}
	}

	for {
		var data []byte
		var done bool
		data, err = c.conn[0].Receive()
		if err != nil {
			if !c.Step1ChannelSecured {
				err = fmt.Errorf("could not secure channel")
			}
			break
		}
		done, err = c.processMessage(data)
		if err != nil {
			break
		}
		if done {
			break
		}
	}
	// a refused manifest is final, and is checked ahead of the purge below:
	// SuccessfulTransfer is set from the peer's TypeFinished, so a refusal the
	// peer can clear is not a refusal.
	if c.refused != nil {
		return c.refused
	}
	if c.SuccessfulTransfer {
		if err != nil {
			log.Debugf("purging error: %s", err)
		}
		err = nil
	}
	if c.Options.IsSender && c.SuccessfulTransfer {
		for _, file := range c.FilesToTransfer {
			if file.TempFile {
				fmt.Println("removing " + file.Name)
				os.Remove(file.Name)
			}
		}
	}

	if c.SuccessfulTransfer && !c.Options.IsSender {
		// `send <folder>` arrives as a zip the receiver unpacks in place.
		// extractZip validates every entry before writing any of them and
		// returns errors instead of calling log.Fatalln, which is what
		// utils.UnzipDirectory does.
		for i, file := range c.FilesToTransfer {
			if !file.TempFile {
				continue
			}
			if extractErr := c.dest.extractZip(c.filePaths[i]); extractErr != nil {
				return fmt.Errorf("extracting %s: %w", file.Name, extractErr)
			}
			if removeErr := c.dest.remove(c.filePaths[i]); removeErr != nil {
				log.Warnf("error removing %s: %v", file.Name, removeErr)
			}
		}
	}

	// Stdout is set from the peer's SendingText, and FilesToTransferCurrentNum
	// is a peer-supplied index, so this needs bounds before it indexes.
	if c.Options.Stdout && !c.Options.IsSender && c.currentFileInRange() {
		rel := c.filePaths[c.FilesToTransferCurrentNum]
		if !c.CurrentFileIsClosed {
			c.CurrentFile.Close()
			c.CurrentFileIsClosed = true
		}
		if err := c.dest.remove(rel); err != nil {
			log.Warnf("error removing %s: %v", rel, err)
		}
		fmt.Print("\n")
	}
	if err != nil && strings.Contains(err.Error(), "pake not successful") {
		err = fmt.Errorf("password mismatch")
	}
	if err != nil && strings.Contains(err.Error(), "unexpected end of JSON input") {
		err = fmt.Errorf("room not ready")
	}
	return
}

// applyFileRequest handles a peer asking for a file. It reports whether the
// request was applied, so the caller knows to skip the rest of the case.
//
// The request is only meaningful to a sender. A receiver acting on one adopts
// a peer-chosen index that the loops after the transfer use to index the
// manifest, which is a panic reachable without any consent from the user. It
// is a separate method so that guard is testable without standing up a key
// exchange.
func (c *Client) applyFileRequest(remoteFile RemoteFileRequest) (applied, done bool, err error) {
	if !c.Options.IsSender {
		log.Debugf("ignoring a file request sent to the receiving side")
		return false, false, nil
	}
	if n := remoteFile.FilesToTransferCurrentNum; n < 0 || n >= len(c.FilesToTransfer) {
		return false, true, fmt.Errorf("peer asked for file %d of %d", n, len(c.FilesToTransfer))
	}
	if err := validateChunkRanges(remoteFile.CurrentFileChunkRanges); err != nil {
		return false, true, err
	}

	c.FilesToTransferCurrentNum = remoteFile.FilesToTransferCurrentNum
	c.CurrentFileChunkRanges = remoteFile.CurrentFileChunkRanges
	c.CurrentFileChunks = utils.ChunkRangesToChunks(c.CurrentFileChunkRanges)
	c.mutex.Lock()
	c.chunkMap = make(map[uint64]struct{})
	for _, chunk := range c.CurrentFileChunks {
		c.chunkMap[uint64(chunk)] = struct{}{}
	}
	c.mutex.Unlock()
	c.Step3RecipientRequestFile = true
	return true, false, nil
}

// validateChunkRanges bounds a peer-supplied resume request before
// utils.ChunkRangesToChunks expands it. That function reads chunkRanges[i+1]
// for each count and multiplies out the totals, so a malformed or huge range
// list is either an out-of-range read or an allocation the peer chose.
func validateChunkRanges(ranges []int64) error {
	if len(ranges) == 0 {
		return nil
	}
	// the shape is [chunkSize, start, count, start, count, ...].
	if len(ranges)%2 != 1 {
		return fmt.Errorf("peer sent a malformed resume request (%d values)", len(ranges))
	}
	if ranges[0] <= 0 {
		return fmt.Errorf("peer sent a resume request with chunk size %d", ranges[0])
	}
	var total int64
	for i := 1; i < len(ranges); i += 2 {
		if ranges[i] < 0 {
			return fmt.Errorf("peer sent a negative resume offset (%d)", ranges[i])
		}
		count := ranges[i+1]
		if count < 0 {
			return fmt.Errorf("peer sent a negative resume length (%d)", count)
		}
		total += count
		if total > maxResumeChunks {
			return fmt.Errorf("peer sent a resume request covering %d chunks", total)
		}
	}
	return nil
}

// maxResumeChunks bounds what a peer may ask to resume. At croc's chunk size
// this is far more than any real transfer needs, and it keeps
// ChunkRangesToChunks from being asked for an arbitrary allocation.
const maxResumeChunks = 1 << 24

// refuse records a rejected manifest, tells the peer, and stops the transfer.
// The wire message keeps upstream's "refusing files" prefix, which a stock croc
// sender matches on to exit cleanly rather than reporting a transport fault.
func (c *Client) refuse(reason string) (done bool, err error) {
	c.refused = &refusalError{reason: "refusing files: " + reason}
	if len(c.conn) > 0 && c.conn[0] != nil {
		if sendErr := message.Send(c.conn[0], c.Key, message.Message{
			Type:    message.TypeError,
			Message: c.refused.Error(),
		}); sendErr != nil {
			log.Debugf("could not tell the peer why: %v", sendErr)
		}
	}
	return true, c.refused
}

// currentFileInRange reports whether FilesToTransferCurrentNum can be used as
// an index. The peer supplies it over the wire, and several of the loops below
// index the manifest with it, so an out-of-range value is a panic reachable
// without any consent from the user.
func (c *Client) currentFileInRange() bool {
	return c.FilesToTransferCurrentNum >= 0 &&
		c.FilesToTransferCurrentNum < len(c.FilesToTransfer) &&
		c.FilesToTransferCurrentNum < len(c.filePaths)
}

func (c *Client) createEmptyFolder(i int) (err error) {
	err = c.dest.mkdirAll(c.folderPaths[i])
	if err != nil {
		return
	}
	fmt.Fprintf(os.Stderr, "%s\n", c.EmptyFoldersToTransfer[i].FolderRemote)
	c.bar = progressbar.NewOptions64(1,
		progressbar.OptionOnCompletion(func() {
			c.fmtPrintUpdate()
		}),
		progressbar.OptionSetWidth(20),
		progressbar.OptionSetDescription(" "),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionSetVisibility(!c.Options.SendingText),
	)
	c.bar.Finish() //nolint
	return
}

func (c *Client) processMessageFileInfo(m message.Message) (done bool, err error) {
	var senderInfo SenderInfo
	err = json.Unmarshal(m.Bytes, &senderInfo)
	if err != nil {
		return
	}
	if c.Options.IsSender || c.dest == nil {
		// only a sender announces a file list. one arriving here means the peer
		// has the roles backwards, and there is no destination to validate it
		// against.
		return c.refuse("a file list arrived from the receiving side")
	}
	if c.Step2FileInfoTransferred {
		// a second list would retarget writes already under way against paths
		// nothing has checked in this state.
		return c.refuse("a second file list arrived mid-transfer")
	}

	// the gate. validated against the destination *before* any of it is staged
	// into the client, because every loop after the transfer indexes this state
	// and a refusal that leaves it populated is not a refusal.
	filePaths, folderPaths, verr := validateManifest(c.dest.root, senderInfo)
	if verr != nil {
		return c.refuse(verr.Error())
	}

	c.Options.SendingText = senderInfo.SendingText
	c.Options.NoCompress = senderInfo.NoCompress
	c.Options.HashAlgorithm = senderInfo.HashAlgorithm
	c.EmptyFoldersToTransfer = senderInfo.EmptyFoldersToTransfer
	c.TotalNumberFolders = senderInfo.TotalNumberFolders
	c.FilesToTransfer = senderInfo.FilesToTransfer
	c.filePaths = filePaths
	c.folderPaths = folderPaths
	c.TotalNumberOfContents = 0
	if c.FilesToTransfer != nil {
		c.TotalNumberOfContents += len(c.FilesToTransfer)
	}
	if c.EmptyFoldersToTransfer != nil {
		c.TotalNumberOfContents += len(c.EmptyFoldersToTransfer)
	}

	if c.Options.HashAlgorithm == "" {
		c.Options.HashAlgorithm = "xxhash"
	}
	if c.Options.SendingText {
		c.Options.Stdout = true
	}

	fname := fmt.Sprintf("%d files", len(c.FilesToTransfer))
	folderName := fmt.Sprintf("%d folders", c.TotalNumberFolders)
	if len(c.FilesToTransfer) == 1 {
		fname = fmt.Sprintf("'%s'", c.FilesToTransfer[0].Name)
	}
	totalSize := int64(0)
	for i, fi := range c.FilesToTransfer {
		totalSize += fi.Size
		if len(fi.Name) > c.longestFilename {
			c.longestFilename = len(fi.Name)
		}
		if strings.HasPrefix(fi.Name, "croc-stdin-") && c.Options.SendingText {
			// the peer decides a name is needed, but must not choose it.
			// utils.RandomFileName would also create it relative to the process
			// working directory rather than the destination.
			var name string
			name, err = c.dest.createTemp("croc-stdin-")
			if err != nil {
				return
			}
			c.FilesToTransfer[i].Name = name
			c.FilesToTransfer[i].FolderRemote = "./"
			c.filePaths[i] = name
		}
	}
	action := "accept"
	if c.Options.SendingText {
		action = "display"
		fname = "text message"
	}
	if !c.Options.NoPrompt || c.Options.Ask || senderInfo.Ask {
		if c.Options.Ask || senderInfo.Ask {
			machID, _ := machineid.ID()
			fmt.Fprintf(os.Stderr, "\ryour machine id is '%s'.\n%s %s (%s) from '%s'? (Y/n) ", machID, action, fname, utils.ByteCountDecimal(totalSize), senderInfo.MachineID)
		} else {
			if c.TotalNumberFolders > 0 {
				fmt.Fprintf(os.Stderr, "\r%s %s and %s (%s)? (Y/n) ", action, fname, folderName, utils.ByteCountDecimal(totalSize))
			} else {
				fmt.Fprintf(os.Stderr, "\r%s %s (%s)? (Y/n) ", action, fname, utils.ByteCountDecimal(totalSize))
			}
		}
		choice := strings.ToLower(utils.GetInput(""))
		if choice != "" && choice != "y" && choice != "yes" {
			err = message.Send(c.conn[0], c.Key, message.Message{
				Type:    message.TypeError,
				Message: "refusing files",
			})
			if err != nil {
				return false, err
			}
			return true, fmt.Errorf("refused files")
		}
	} else {
		fmt.Fprintf(os.Stderr, "\rreceiving %s (%s) \n", fname, utils.ByteCountDecimal(totalSize))
	}
	fmt.Fprintf(os.Stderr, "\nreceiving (<-%s)\n", c.ExternalIPConnected)

	for i := range c.EmptyFoldersToTransfer {
		declared := c.EmptyFoldersToTransfer[i].FolderRemote
		// three ways, not two: os.Root reports a pre-existing symlink out of
		// the destination as an error rather than as absent, and validation
		// already refused those, so one here is worth reporting not ignoring.
		_, ex, lerr := c.dest.lstat(c.folderPaths[i])
		if lerr != nil {
			return c.refuse(fmt.Sprintf("cannot inspect %q in the destination: %v", declared, lerr))
		}
		if ex == absent {
			if err = c.createEmptyFolder(i); err != nil {
				return
			}
			continue
		}

		isEmpty, emptyErr := c.dest.isEmptyDir(c.folderPaths[i])
		if emptyErr != nil || isEmpty {
			// already an empty directory, or unreadable; MkdirAll would be a
			// no-op either way.
			continue
		}
		if c.Options.NoPrompt {
			// receive hardcodes NoPrompt, and this prompt read stdin
			// regardless, which is the only way a receive could hang. the
			// directory keeps its contents; nothing is emptied behind the
			// user's back.
			fmt.Fprintf(os.Stderr, "keeping the existing contents of %s\n", declared)
			continue
		}
		prompt := fmt.Sprintf("\n%s already has some content in it. \ndo you want"+
			" to overwrite it with an empty folder? (y/N) ", declared)
		if choice := strings.ToLower(utils.GetInput(prompt)); choice == "y" || choice == "yes" {
			if err = c.createEmptyFolder(i); err != nil {
				return
			}
		}
	}

	if c.FilesToTransfer == nil {
		c.SuccessfulTransfer = true
		c.Step3RecipientRequestFile = true
		c.Step4FileTransferred = true
		errStopTransfer := message.Send(c.conn[0], c.Key, message.Message{
			Type: message.TypeFinished,
		})
		if errStopTransfer != nil {
			err = errStopTransfer
		}
	}
	c.Step2FileInfoTransferred = true
	return
}

func (c *Client) processMessagePake(m message.Message) (err error) {
	var salt []byte
	if c.Options.IsSender {
		c.Pake, err = pake.InitCurve([]byte(c.Options.SharedSecret[5:]), 1, string(m.Bytes2))
		if err != nil {
			return
		}

		err = c.Pake.Update(m.Bytes)
		if err != nil {
			return
		}

		salt = make([]byte, 8)
		if _, rerr := rand.Read(salt); rerr != nil {
			return rerr
		}
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type:   message.TypePAKE,
			Bytes:  c.Pake.Bytes(),
			Bytes2: salt,
		})
	} else {
		err = c.Pake.Update(m.Bytes)
		if err != nil {
			return
		}
		salt = m.Bytes2
	}
	key, err := c.Pake.SessionKey()
	if err != nil {
		return err
	}
	c.Key, _, err = crypt.New(key, salt)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(len(c.Options.RelayPorts))
	for i := 0; i < len(c.Options.RelayPorts); i++ {
		go func(j int) {
			defer wg.Done()
			var host string
			if c.Options.RelayAddress == "localhost" {
				host = c.Options.RelayAddress
			} else {
				host, _, err = net.SplitHostPort(c.Options.RelayAddress)
				if err != nil {
					return
				}
			}
			server := net.JoinHostPort(host, c.Options.RelayPorts[j])
			c.conn[j+1], _, _, err = tcp.ConnectToTCPServer(
				server,
				c.Options.RelayPassword,
				fmt.Sprintf("%s-%d", utils.SHA256(c.Options.SharedSecret[:5])[:6], j),
			)
			if err != nil {
				panic(err)
			}
			if !c.Options.IsSender {
				go c.receiveData(j)
			}
		}(i)
	}
	wg.Wait()

	if !c.Options.IsSender {
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type:    message.TypeExternalIP,
			Message: c.ExternalIP,
			Bytes:   m.Bytes,
		})
	}
	return
}

func (c *Client) processExternalIP(m message.Message) (done bool, err error) {
	if c.Options.IsSender {
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type:    message.TypeExternalIP,
			Message: c.ExternalIP,
		})
		if err != nil {
			return true, err
		}
	}
	if c.ExternalIPConnected == "" {
		c.ExternalIPConnected = m.Message
	}
	c.Step1ChannelSecured = true
	return
}

func (c *Client) processMessage(payload []byte) (done bool, err error) {
	m, err := message.Decode(c.Key, payload)
	if err != nil {
		return
	}

	if m.Type != message.TypePAKE && c.Key == nil {
		err = fmt.Errorf("unencrypted communication rejected")
		done = true
		return
	}

	// a refusal ends the conversation. without this a peer could follow a
	// rejected manifest with TypeFinished, which sets SuccessfulTransfer and
	// would otherwise clear the error further down.
	if c.refused != nil {
		return true, c.refused
	}

	switch m.Type {
	case message.TypeFinished:
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type: message.TypeFinished,
		})
		done = true
		c.SuccessfulTransfer = true
		return
	case message.TypePAKE:
		err = c.processMessagePake(m)
		if err != nil {
			err = fmt.Errorf("pake not successful: %w", err)
		}
	case message.TypeExternalIP:
		done, err = c.processExternalIP(m)
	case message.TypeError:
		fmt.Print("\r")
		err = fmt.Errorf("peer error: %s", m.Message)
		return true, err
	case message.TypeFileInfo:
		done, err = c.processMessageFileInfo(m)
	case message.TypeRecipientReady:
		var remoteFile RemoteFileRequest
		err = json.Unmarshal(m.Bytes, &remoteFile)
		if err != nil {
			return
		}
		var applied bool
		applied, done, err = c.applyFileRequest(remoteFile)
		if err != nil || !applied {
			return
		}

		if c.Options.Ask {
			fmt.Fprintf(os.Stderr, "send to machine '%s'? (Y/n) ", remoteFile.MachineID)
			choice := strings.ToLower(utils.GetInput(""))
			if choice != "" && choice != "y" && choice != "yes" {
				err = message.Send(c.conn[0], c.Key, message.Message{
					Type:    message.TypeError,
					Message: "refusing files",
				})
				done = true
				return
			}
		}
	case message.TypeCloseSender:
		c.bar.Finish() //nolint
		c.Step4FileTransferred = false
		c.Step3RecipientRequestFile = false
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type: message.TypeCloseRecipient,
		})
	case message.TypeCloseRecipient:
		c.Step4FileTransferred = false
		c.Step3RecipientRequestFile = false
	}
	if err != nil {
		return
	}
	err = c.updateState()
	return
}

func (c *Client) updateIfSenderChannelSecured() (err error) {
	if c.Options.IsSender && c.Step1ChannelSecured && !c.Step2FileInfoTransferred {
		var b []byte
		machID, _ := machineid.ID()
		b, err = json.Marshal(SenderInfo{
			FilesToTransfer:        c.FilesToTransfer,
			EmptyFoldersToTransfer: c.EmptyFoldersToTransfer,
			MachineID:              machID,
			Ask:                    c.Options.Ask,
			TotalNumberFolders:     c.TotalNumberFolders,
			SendingText:            c.Options.SendingText,
			NoCompress:             c.Options.NoCompress,
			HashAlgorithm:          c.Options.HashAlgorithm,
		})
		if err != nil {
			return
		}
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type:  message.TypeFileInfo,
			Bytes: b,
		})
		if err != nil {
			return
		}

		c.Step2FileInfoTransferred = true
	}
	return
}

func (c *Client) recipientInitializeFile() (err error) {
	if !c.currentFileInRange() {
		return fmt.Errorf("peer asked for file %d of %d", c.FilesToTransferCurrentNum, len(c.FilesToTransfer))
	}
	rel := c.filePaths[c.FilesToTransferCurrentNum]
	size := c.FilesToTransfer[c.FilesToTransferCurrentNum].Size

	if dir := path.Dir(rel); dir != "." {
		if err := c.dest.mkdirAll(dir); err != nil {
			log.Errorf("can't create %s: %v", dir, err)
		}
	}

	var existed bool
	c.CurrentFile, existed, err = c.dest.openForWrite(rel)
	if err != nil {
		return fmt.Errorf("could not open %s: %w", rel, err)
	}

	c.CurrentFileChunks = []int64{}
	c.CurrentFileChunkRanges = []int64{}
	truncate := true
	if existed {
		stat, _ := c.CurrentFile.Stat()
		truncate = stat.Size() != size
		if !truncate {
			c.CurrentFileChunkRanges = c.dest.missingChunks(rel, size, models.TCP_BUFFER_SIZE/2)
		}
	}
	if truncate {
		// size was checked non-negative by validateManifest; Truncate(-1)
		// would otherwise fail only after the file existed.
		if err := c.CurrentFile.Truncate(size); err != nil {
			return fmt.Errorf("could not truncate %s: %w", rel, err)
		}
	}
	return
}

func (c *Client) recipientGetFileReady(finished bool) (err error) {
	if finished {
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type: message.TypeFinished,
		})
		if err != nil {
			panic(err)
		}
		c.SuccessfulTransfer = true
		c.FilesHasFinished[c.FilesToTransferCurrentNum] = struct{}{}
	}

	err = c.recipientInitializeFile()
	if err != nil {
		return
	}

	c.TotalSent = 0
	c.CurrentFileIsClosed = false
	machID, _ := machineid.ID()
	bRequest, _ := json.Marshal(RemoteFileRequest{
		CurrentFileChunkRanges:    c.CurrentFileChunkRanges,
		FilesToTransferCurrentNum: c.FilesToTransferCurrentNum,
		MachineID:                 machID,
	})
	c.CurrentFileChunks = utils.ChunkRangesToChunks(c.CurrentFileChunkRanges)

	if !finished {
		c.setBar()
	}

	err = message.Send(c.conn[0], c.Key, message.Message{
		Type:  message.TypeRecipientReady,
		Bytes: bRequest,
	})
	if err != nil {
		return
	}
	c.Step3RecipientRequestFile = true
	return
}

func (c *Client) createEmptyFileAndFinish(fileInfo FileInfo, i int) (err error) {
	if i < 0 || i >= len(c.filePaths) {
		return fmt.Errorf("peer asked for file %d of %d", i, len(c.filePaths))
	}
	rel := c.filePaths[i]
	if dir := path.Dir(rel); dir != "." {
		if err = c.dest.mkdirAll(dir); err != nil {
			return
		}
	}
	if fileInfo.Symlink != "" {
		// the target was checked by validateManifest. os.Root would create a
		// link out of the destination without complaint, and it would outlive
		// the transfer for whatever walks the tree next.
		if err = c.dest.symlink(fileInfo.Symlink, rel); err != nil {
			return
		}
	} else if err = c.dest.createEmpty(rel); err != nil {
		return
	}
	description := fmt.Sprintf("%-*s", c.longestFilename, c.FilesToTransfer[i].Name)
	if len(c.FilesToTransfer) == 1 {
		description = c.FilesToTransfer[i].Name
	} else {
		description = " " + description
	}
	c.bar = progressbar.NewOptions64(1,
		progressbar.OptionOnCompletion(func() {
			c.fmtPrintUpdate()
		}),
		progressbar.OptionSetWidth(20),
		progressbar.OptionSetDescription(description),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionSetVisibility(!c.Options.SendingText),
	)
	c.bar.Finish() //nolint
	return
}

func (c *Client) updateIfRecipientHasFileInfo() (err error) {
	if !(!c.Options.IsSender && c.Step2FileInfoTransferred && !c.Step3RecipientRequestFile) {
		return
	}
	finished := true
	for i, fileInfo := range c.FilesToTransfer {
		if _, ok := c.FilesHasFinished[i]; ok {
			continue
		}
		if i < c.FilesToTransferCurrentNum {
			continue
		}
		if i >= len(c.filePaths) {
			return fmt.Errorf("file %d has no validated destination", i)
		}
		rel := c.filePaths[i]

		// three ways: a pre-existing symlink out of the destination is an error
		// from os.Root rather than an absence. treating it as absent and
		// falling back to a plain os.Stat on the raw path is exactly the hole
		// the root was opened to close.
		recipientFileInfo, ex, lerr := c.dest.lstat(rel)
		if lerr != nil {
			return fmt.Errorf("cannot inspect %s in the destination: %w", rel, lerr)
		}
		errRecipientFile := fs.ErrNotExist
		if ex == present {
			errRecipientFile = nil
		}
		var errHash error
		var fileHash []byte
		if errRecipientFile == nil && recipientFileInfo.Size() == fileInfo.Size {
			fileHash, errHash = c.dest.hashFile(rel, c.Options.HashAlgorithm)
		}
		if fileInfo.Size == 0 || fileInfo.Symlink != "" {
			err = c.createEmptyFileAndFinish(fileInfo, i)
			if err != nil {
				return
			} else {
				c.numberOfTransferredFiles++
			}
			continue
		}
		if !bytes.Equal(fileHash, fileInfo.Hash) {
			if errHash == nil && !c.Options.Overwrite && errRecipientFile == nil && !strings.HasPrefix(fileInfo.Name, "croc-stdin-") && !c.Options.SendingText {
				missingChunks := utils.ChunkRangesToChunks(c.dest.missingChunks(
					rel,
					fileInfo.Size,
					models.TCP_BUFFER_SIZE/2,
				))
				percentDone := 100 - float64(len(missingChunks)*models.TCP_BUFFER_SIZE/2)/float64(fileInfo.Size)*100

				prompt := fmt.Sprintf("\noverwrite '%s'? (y/N) ", rel)
				if percentDone < 99 {
					prompt = fmt.Sprintf("\nresume '%s' (%2.1f%%)? (y/N) ", rel, percentDone)
				}
				choice := strings.ToLower(utils.GetInput(prompt))
				if choice != "y" && choice != "yes" {
					fmt.Fprintf(os.Stderr, "skipping '%s'", rel)
					continue
				}
			}
		}
		if errHash != nil || !bytes.Equal(fileHash, fileInfo.Hash) {
			finished = false
			c.FilesToTransferCurrentNum = i
			c.numberOfTransferredFiles++
			newFolder, _ := filepath.Split(fileInfo.FolderRemote)
			if newFolder != c.LastFolder && len(c.FilesToTransfer) > 0 && !c.Options.SendingText && newFolder != "./" {
				fmt.Fprintf(os.Stderr, "\r%s\n", newFolder)
			}
			c.LastFolder = newFolder
			break
		}
	}
	c.recipientGetFileReady(finished) //nolint
	return
}

func (c *Client) fmtPrintUpdate() {
	c.finishedNum++
	if c.TotalNumberOfContents > 1 {
		fmt.Fprintf(os.Stderr, " %d/%d\n", c.finishedNum, c.TotalNumberOfContents)
	} else {
		fmt.Fprintf(os.Stderr, "\n")
	}
}

func (c *Client) updateState() (err error) {
	err = c.updateIfSenderChannelSecured()
	if err != nil {
		return
	}

	err = c.updateIfRecipientHasFileInfo()
	if err != nil {
		return
	}

	if c.Options.IsSender && c.Step3RecipientRequestFile && !c.Step4FileTransferred {
		if !c.firstSend {
			fmt.Fprintf(os.Stderr, "\nsending (->%s)\n", c.ExternalIPConnected)
			c.firstSend = true
			for i := range c.FilesToTransfer {
				if c.FilesToTransfer[i].Size == 0 {
					description := fmt.Sprintf("%-*s", c.longestFilename, c.FilesToTransfer[i].Name)
					if len(c.FilesToTransfer) == 1 {
						description = c.FilesToTransfer[i].Name
					}
					c.bar = progressbar.NewOptions64(1,
						progressbar.OptionOnCompletion(func() {
							c.fmtPrintUpdate()
						}),
						progressbar.OptionSetWidth(20),
						progressbar.OptionSetDescription(description),
						progressbar.OptionSetRenderBlankState(true),
						progressbar.OptionShowBytes(true),
						progressbar.OptionShowCount(),
						progressbar.OptionSetWriter(os.Stderr),
						progressbar.OptionSetVisibility(!c.Options.SendingText),
					)
					c.bar.Finish() //nolint
				}
			}
		}
		c.Step4FileTransferred = true
		c.setBar()
		c.TotalSent = 0
		c.CurrentFileIsClosed = false
		pathToFile := path.Join(
			c.FilesToTransfer[c.FilesToTransferCurrentNum].FolderSource,
			c.FilesToTransfer[c.FilesToTransferCurrentNum].Name,
		)
		c.fread, err = os.Open(pathToFile)
		c.numfinished = 0
		if err != nil {
			return
		}
		for i := 0; i < len(c.Options.RelayPorts); i++ {
			go c.sendData(i)
		}
	}
	return
}

func (c *Client) setBar() {
	description := fmt.Sprintf("%-*s", c.longestFilename, c.FilesToTransfer[c.FilesToTransferCurrentNum].Name)
	folder, _ := filepath.Split(c.FilesToTransfer[c.FilesToTransferCurrentNum].FolderRemote)
	if folder == "./" {
		description = c.FilesToTransfer[c.FilesToTransferCurrentNum].Name
	} else if !c.Options.IsSender {
		description = " " + description
	}
	c.bar = progressbar.NewOptions64(
		c.FilesToTransfer[c.FilesToTransferCurrentNum].Size,
		progressbar.OptionOnCompletion(func() {
			c.fmtPrintUpdate()
		}),
		progressbar.OptionSetWidth(20),
		progressbar.OptionSetDescription(description),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionThrottle(100*time.Millisecond),
		progressbar.OptionSetVisibility(!c.Options.SendingText),
	)
	byteToDo := int64(len(c.CurrentFileChunks) * models.TCP_BUFFER_SIZE / 2)
	if byteToDo > 0 {
		bytesDone := c.FilesToTransfer[c.FilesToTransferCurrentNum].Size - byteToDo
		if bytesDone > 0 {
			c.bar.Add64(bytesDone) //nolint
		}
	}
}

func (c *Client) receiveData(i int) {
	for {
		data, err := c.conn[i+1].Receive()
		if err != nil {
			break
		}
		if bytes.Equal(data, []byte{1}) {
			continue
		}

		data, err = crypt.Decrypt(data, c.Key)
		if err != nil {
			log.Errorf("could not decrypt a chunk: %v", err)
			return
		}
		if !c.Options.NoCompress {
			data = compress.Decompress(data)
		}

		// the frame is an 8 byte offset followed by the payload. a shorter one
		// used to be a slice bounds panic on this goroutine, which takes the
		// whole process with it.
		if len(data) < 8 {
			log.Errorf("peer sent a %d byte chunk, too short to carry an offset", len(data))
			return
		}
		var position uint64
		rbuf := bytes.NewReader(data[:8])
		err = binary.Read(rbuf, binary.LittleEndian, &position)
		if err != nil {
			log.Errorf("could not read a chunk offset: %v", err)
			return
		}
		positionInt64 := int64(position)
		payload := data[8:]

		// the offset is peer-supplied and goes straight into WriteAt, so
		// without a bound the peer can write anywhere in the file, or grow it
		// well past the size it declared and the receiver accepted.
		if !c.currentFileInRange() {
			log.Errorf("peer sent a chunk for file %d of %d", c.FilesToTransferCurrentNum, len(c.FilesToTransfer))
			return
		}
		declaredSize := c.FilesToTransfer[c.FilesToTransferCurrentNum].Size
		if positionInt64 < 0 || positionInt64+int64(len(payload)) > declaredSize {
			log.Errorf("peer sent %d bytes at offset %d, past the declared size %d", len(payload), positionInt64, declaredSize)
			return
		}

		c.mutex.Lock()
		_, err = c.CurrentFile.WriteAt(payload, positionInt64)
		if err != nil {
			c.mutex.Unlock()
			log.Errorf("could not write a chunk: %v", err)
			return
		}
		c.bar.Add(len(payload)) //nolint
		c.TotalSent += int64(len(payload))
		c.TotalChunksTransferred++

		if !c.CurrentFileIsClosed && (c.TotalChunksTransferred == len(c.CurrentFileChunks) || c.TotalSent == c.FilesToTransfer[c.FilesToTransferCurrentNum].Size) {
			c.CurrentFileIsClosed = true
			if err := c.CurrentFile.Close(); err != nil {
				log.Debugf("error closing %s: %v", c.CurrentFile.Name(), err)
			}
			if c.Options.Stdout || c.Options.SendingText {
				b, readErr := c.dest.readFile(c.filePaths[c.FilesToTransferCurrentNum])
				if readErr != nil {
					log.Debugf("error reading back the received file: %v", readErr)
				}
				fmt.Print(string(b))
			}
			err = message.Send(c.conn[0], c.Key, message.Message{
				Type: message.TypeCloseSender,
			})
			if err != nil {
				panic(err)
			}
		}
		c.mutex.Unlock()
	}
}

func (c *Client) sendData(i int) {
	defer func() {
		c.numfinished++
		if c.numfinished == len(c.Options.RelayPorts) {
			if err := c.fread.Close(); err != nil {
				log.Errorf("error closing file: %v", err)
			}
		}
	}()

	var readingPos int64
	pos := uint64(0)
	curi := float64(0)
	for {
		data := make([]byte, models.TCP_BUFFER_SIZE/2)
		n, errRead := c.fread.ReadAt(data, readingPos)
		readingPos += int64(n)
		if c.limiter != nil {
			r := c.limiter.ReserveN(time.Now(), n)
			time.Sleep(r.Delay())
		}

		if math.Mod(curi, float64(len(c.Options.RelayPorts))) == float64(i) {
			usableChunk := true
			c.mutex.Lock()
			if len(c.chunkMap) != 0 {
				if _, ok := c.chunkMap[pos]; !ok {
					usableChunk = false
				} else {
					delete(c.chunkMap, pos)
				}
			}
			c.mutex.Unlock()
			if usableChunk {
				posByte := make([]byte, 8)
				binary.LittleEndian.PutUint64(posByte, pos)
				var err error
				var dataToSend []byte
				if c.Options.NoCompress {
					dataToSend, err = crypt.Encrypt(
						append(posByte, data[:n]...),
						c.Key,
					)
				} else {
					dataToSend, err = crypt.Encrypt(
						compress.Compress(
							append(posByte, data[:n]...),
						),
						c.Key,
					)
				}
				if err != nil {
					panic(err)
				}

				err = c.conn[i+1].Send(dataToSend)
				if err != nil {
					panic(err)
				}
				c.bar.Add(n) //nolint
				c.TotalSent += int64(n)
			}
		}

		curi++
		pos += uint64(n)

		if errRead != nil {
			if errRead == io.EOF {
				break
			}
			panic(errRead)
		}
	}
}

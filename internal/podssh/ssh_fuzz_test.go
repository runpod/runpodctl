package podssh

import (
	"slices"
	"strings"
	"testing"
)

// FuzzRsyncConnectionLost checks the one invariant a retry depends on: a
// refused host key is never explained as a restarting pod, whatever else the
// output carries.
func FuzzRsyncConnectionLost(f *testing.F) {
	for _, s := range []string{
		"ssh: connect to host 1.2.3.4 port 40022: Connection refused",
		"kex_exchange_identification: read: Connection reset by peer",
		"Connection to 1.2.3.4 closed by remote host.\nrsync: unexpected end of file",
		"rsync: connection unexpectedly closed (0 bytes received so far) [sender]",
		"root@1.2.3.4: Permission denied (publickey).",
		"",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, output string) {
		for _, refused := range []string{
			"Host key verification failed.\n" + output,
			output + "\nHost key verification failed.",
		} {
			if rsyncConnectionLost(refused) {
				t.Fatalf("rsyncConnectionLost(%q) = true for a refused host key", refused)
			}
		}
	})
}

// FuzzRsyncRemoteShell checks that rsync's -e splitting gives back exactly the
// arguments rsyncRemoteShell joined, and that a UserKnownHostsFile value passed
// through quoteSSHConfigValue is then one token to openssh.
//
// Out of the domain: an empty or NUL-carrying argument (rsync drops the first
// and cannot carry the second), and, for the openssh value, quotes,
// backslashes and whitespace other than space and tab, which
// quoteSSHConfigValue does not escape.
func FuzzRsyncRemoteShell(f *testing.F) {
	for _, seed := range [][3]string{
		{"/home/o'brien/my key", "/a b/kh", "22"},
		{`it's "quoted"`, "/plain/kh", "-p"},
		{"''", "/tab\tsep/kh", "x y"},
	} {
		f.Add(seed[0], seed[1], seed[2])
	}
	f.Fuzz(func(t *testing.T, key, knownHosts, extra string) {
		args := []string{"ssh", "-i", key, "-o", "UserKnownHostsFile=" + quoteSSHConfigValue(knownHosts), extra}
		for _, arg := range args {
			if arg == "" || strings.ContainsRune(arg, 0) {
				return
			}
		}
		shell := rsyncRemoteShell(args)
		if got := splitRsyncRemoteShell(shell); !slices.Equal(got, args) {
			t.Fatalf("rsync splits %q into %q, want %q", shell, got, args)
		}

		if knownHosts == "" || strings.ContainsAny(knownHosts, "\"'\\\r\n\v\f") {
			return
		}
		value := strings.TrimPrefix(args[4], "UserKnownHostsFile=")
		if got := splitOpenSSHValue(value); !slices.Equal(got, []string{knownHosts}) {
			t.Fatalf("openssh splits %q into %q, want the one path %q", value, got, knownHosts)
		}
	})
}

// splitRsyncRemoteShell is rsync's split of the -e string (do_cmd in
// main.c): runs separated by spaces, single or double quotes grouping, a
// doubled quote inside a quoted run standing for itself.
func splitRsyncRemoteShell(s string) []string {
	var (
		args  []string
		cur   strings.Builder
		inArg bool
		quote byte
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c != quote {
				cur.WriteByte(c)
			} else if i+1 < len(s) && s[i+1] == quote {
				cur.WriteByte(c)
				i++
			} else {
				quote = 0
			}
		case c == ' ':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		case c == '\'' || c == '"':
			quote = c
			inArg = true
		default:
			cur.WriteByte(c)
			inArg = true
		}
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args
}

// splitOpenSSHValue is openssh's whitespace split of a config value
// (argv_split in misc.c) over the domain above: space and tab separate, and a
// double-quoted run is one token.
func splitOpenSSHValue(s string) []string {
	var (
		args   []string
		cur    strings.Builder
		inArg  bool
		quoted bool
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			quoted = !quoted
			inArg = true
		case !quoted && (c == ' ' || c == '\t'):
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteByte(c)
			inArg = true
		}
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args
}

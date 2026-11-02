package escptest

import (
	"github.com/davidlukac/escp-go/internal/wire"
)

// BackupMode selects how the switch misbehaves when the configuration is read.
type BackupMode string

// The ways a configuration backup can go wrong; BackupOK is the well-behaved switch.
const (
	BackupOK         BackupMode = ""
	BackupBadMagic   BackupMode = "badmagic"   // the file does not start with the configuration magic
	BackupShort      BackupMode = "short"      // only the magic, no body
	BackupDropSecond BackupMode = "dropsecond" // the last fragment never arrives
	BackupDropFirst  BackupMode = "dropfirst"  // only the last fragment arrives
	BackupSilent     BackupMode = "silent"     // no reply at all
	BackupFlood      BackupMode = "flood"      // fragments that never end
	BackupCorrupt    BackupMode = "corrupt"    // every packet arrives, but one byte of the file is wrong
	BackupDuplicate  BackupMode = "duplicate"  // the first fragment arrives twice
)

// SetBackupMode chooses how the configuration backup misbehaves (BackupOK, the default, is a
// well-behaved switch); it applies to the next backup read.
func (f *Switch) SetBackupMode(mode BackupMode) {
	f.mu.Lock()
	f.backupMode = mode
	f.mu.Unlock()
}

// SetBackupScript makes the next backup requests misbehave one mode each, in order (BackupOK for a
// request that goes well); after the last one the mode of SetBackupMode applies again. It plays a
// loss that happens once, as on a real network.
func (f *Switch) SetBackupScript(modes ...BackupMode) {
	f.mu.Lock()
	f.backupScript = append([]BackupMode(nil), modes...)
	f.mu.Unlock()
}

// BackupFile is a 2258-byte stand-in for the switch's configuration file.
func BackupFile() []byte {
	b := make([]byte, 2258)
	copy(b, wire.ConfigMagic)
	for i := 4; i < len(b); i++ {
		b[i] = byte(i * 7)
	}
	return b
}

// configChunk is how many bytes of the configuration file one packet carries.
const configChunk = 1400

// handleConfigRead sends the configuration file in two fragments, or misbehaves as the test asked
// (BackupMode).
func (f *Switch) handleConfigRead(r request) {
	if !f.credentialsOK(r) {
		f.replyBadCredentials(r)
		return
	}
	f.mu.Lock()
	mode := f.backupMode
	if len(f.backupScript) > 0 {
		mode, f.backupScript = f.backupScript[0], f.backupScript[1:]
	}
	f.mu.Unlock()
	file := func(h wire.Header, part []byte) {
		answer := r
		answer.h = h
		f.reply(answer, wire.OpSetReply, 0, []wire.TLV{{Type: wire.TLVConfigFile, Value: part}}, true)
	}
	if mode == BackupSilent {
		return
	}
	if mode == BackupFlood {
		// A sender that never clears the fragment flag.
		more := r.h
		more.Fragment = 1
		for range 100 {
			file(more, make([]byte, 1000))
		}
		return
	}
	cfg := BackupFile()
	switch mode {
	case BackupBadMagic:
		cfg[0] = 0
	case BackupShort:
		cfg = cfg[:4]
	case BackupCorrupt:
		cfg[configChunk+100] ^= 0xff // in the second fragment
	default: // the file arrives whole, or the fragments are dropped or doubled below
	}
	first, second := cfg, []byte(nil)
	if len(cfg) > configChunk {
		first, second = cfg[:configChunk], cfg[configChunk:]
	}
	firstHeader := r.h
	firstHeader.Fragment = 1
	if second == nil {
		firstHeader.Fragment = 0
	}
	if mode != BackupDropFirst {
		file(firstHeader, first)
	}
	if mode == BackupDuplicate {
		file(firstHeader, first)
	}
	if second != nil && mode != BackupDropSecond {
		file(r.h, second)
	}
}

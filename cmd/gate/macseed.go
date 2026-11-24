package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// macSeedSources are what a compiler reads to build self/, which the Mac is given with the compiler it proves.
var macSeedSources = []string{"self", "runtime", "stdlib"}

// proveMacSeed holds the macOS compiler that the renewal cross-built to what the Linux one is held to, on the Mac, which
// the Windows machine reaches through the share as the Mac lane does: there the compiler builds a C1 that must be its own
// bytes, C1 builds a C2 and C2 a C3 identical to it, each under the seed's file name, which a Mach-O executable is signed
// under. When the share holds the Mac's proof of exactly this compiler (the digest of its bytes) it is proven. Otherwise
// the compiler, the sources and cmd/gate/macseed.sh are staged at in\seed for the Mac session to run, and the renewal
// refuses until it has, as it does when the share is not mounted: no seed is replaced before all three are proven.
func proveMacSeed(candidate string) error {
	mounted, err := shareIsDir(filepath.Join(macShare, "in"))
	if err != nil {
		return fmt.Errorf("seed not renewed: %w", err)
	}
	if !mounted {
		return fmt.Errorf("seed not renewed: the macOS compiler is proven on the Mac, through the share %s, which is not mounted", macShare)
	}
	data, err := os.ReadFile(candidate)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	have, err := shareReadFile(filepath.Join(macShare, "out", "seed", "proven.txt"))
	if isShareTimeout(err) {
		return fmt.Errorf("seed not renewed: %w", err)
	}
	if err == nil && strings.TrimRight(string(have), "\r\n") == digest {
		return nil
	}
	root, err := scratchDir("minimaxon-mac-seed-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	for _, dir := range macSeedSources {
		if err := robocopy(context.Background(), dir, filepath.Join(root, dir), "/E"); err != nil {
			return fmt.Errorf("seed not renewed: cannot stage %s for the Mac: %w", dir, err)
		}
	}
	if err := copyFile(candidate, filepath.Join(root, macosSeedName)); err != nil {
		return err
	}
	if err := copyFile(filepath.Join("cmd", "gate", "macseed.sh"), filepath.Join(root, "run.sh")); err != nil {
		return err
	}
	// A mirror, so nothing of an older candidate stays for the Mac to prove.
	stage := filepath.Join(macShare, "in", "seed")
	if err := shareCopy(root, stage, "/MIR"); err != nil {
		return fmt.Errorf("seed not renewed: cannot stage the macOS compiler at %s: %w", stage, err)
	}
	return fmt.Errorf("seed not renewed: the Mac has not proven the macOS compiler %s: run bash ~/Dev/agents/minimaxon/in/seed/run.sh there and the renewal again", digest[:12])
}

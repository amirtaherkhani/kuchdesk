package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// A private local receipt binds the input fingerprint to the exact store image
// ID observed after this workflow built and verified it. Docker IDs can denote
// a root/index or config object; no equivalence with registry/CRI IDs is assumed.
type buildReceipt struct {
	Version     int    `json:"version"`
	Store       string `json:"store"`
	Image       string `json:"image"`
	Platform    string `json:"platform"`
	Fingerprint string `json:"fingerprint"`
	ImageID     string `json:"imageId"`
}

func (w Workflow) buildReceiptPath(receipt buildReceipt, create bool) (string, error) {
	dir := w.BuildReceiptDir
	if dir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(cache, "kuchdesk", "build-receipts")
	}
	if create {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", err
		}
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return "", fmt.Errorf("build receipt directory must be private")
	}
	sum := sha256.Sum256([]byte(receipt.Store + "\x00" + receipt.Image + "\x00" + receipt.Platform + "\x00" + receipt.Fingerprint))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".json"), nil
}
func (w Workflow) buildReceiptMatches(expected buildReceipt) bool {
	path, err := w.buildReceiptPath(expected, false)
	if err != nil {
		return false
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 4096 {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 4097))
	decoder.DisallowUnknownFields()
	var recorded buildReceipt
	if decoder.Decode(&recorded) != nil {
		return false
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return false
	}
	return recorded == expected
}
func (w Workflow) writeBuildReceipt(receipt buildReceipt) error {
	path, err := w.buildReceiptPath(receipt, true)
	if err != nil {
		return fmt.Errorf("verified build receipt unavailable: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".build-receipt-*")
	if err != nil {
		return fmt.Errorf("verified build receipt unavailable: %w", err)
	}
	defer os.Remove(file.Name())
	if err = json.NewEncoder(file).Encode(receipt); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(file.Name(), path)
	}
	if err != nil {
		return fmt.Errorf("verified build receipt unavailable: %w", err)
	}
	return nil
}

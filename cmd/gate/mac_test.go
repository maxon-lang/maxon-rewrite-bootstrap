package main

import (
	"errors"
	"testing"
	"time"
)

func TestShareDoGivesUpOnABlockedOperation(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	_, err := shareDo("blocking", 10*time.Millisecond, func() (int, error) {
		<-release
		return 1, nil
	})
	if !isShareTimeout(err) {
		t.Fatalf("a blocked operation gave %v, want a share timeout", err)
	}
}

func TestShareDoGivesTheResultOfAnOperationInTime(t *testing.T) {
	want := errors.New("failed")
	got, err := shareDo("quick", time.Second, func() (int, error) { return 7, want })
	if got != 7 || err != want {
		t.Fatalf("got %d, %v; want 7, %v", got, err, want)
	}
}

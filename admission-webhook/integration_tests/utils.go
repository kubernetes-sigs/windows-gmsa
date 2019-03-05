package integrationtests

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os/exec"
	"testing"
)

func runCommandOrFail(t *testing.T, name string, args ...string) {
	success, stdout, stderr := runCommand(t, name, args...)
	if !success {
		t.Fatal(stdout, stderr)
	}
	fmt.Print(stdout)
}

func runCommand(t *testing.T, name string, args ...string) (success bool, stdout string, stderr string) {
	cmd := exec.Command(name, args...)
	stdoutReader, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrReader, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}

	success = true
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	stdoutBytes, err := ioutil.ReadAll(stdoutReader)
	if err != nil {
		t.Fatal(err)
	}
	stderrBytes, err := ioutil.ReadAll(stderrReader)
	if err != nil {
		t.Fatal(err)
	}

	if err := cmd.Wait(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			success = false
		} else {
			t.Fatal(err)
		}
	}

	return success, string(stdoutBytes), string(stderrBytes)
}

func randomHexString(t *testing.T, length int) string {
	b := length / 2
	randBytes := make([]byte, b)

	if n, err := rand.Reader.Read(randBytes); err != nil || n != b {
		if err == nil {
			err = fmt.Errorf("only got %v random bytes, expected %v", n, b)
		}
		t.Fatal(err)
	}

	return hex.EncodeToString(randBytes)
}

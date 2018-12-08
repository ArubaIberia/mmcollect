package main

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// sshInteract runs an interactive command via SSH
func sshInteract(addr, user, pass, cmd, stdin string) (string, error) {
	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(pass),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		BannerCallback:  ssh.BannerDisplayStderr(),
	})
	if err != nil {
		return "", decorate(err, "Failed to SSH to", addr)
	}
	defer client.Close()
	out, err := runCommand(client, cmd, stdin)
	if err != nil {
		return "", err
	}
	return out, nil
}

func runCommand(client *ssh.Client, command, stdin string) (string, error) {
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	var b bytes.Buffer
	var e bytes.Buffer
	if stdin != "" {
		command = fmt.Sprintf("%s\n%s\n", command, stdin)
	} else {
		command = fmt.Sprintf("%s\n", command)
	}
	sess.Stdin = strings.NewReader(command)
	sess.Stdout = &b
	sess.Stderr = &e
	if err = sess.Shell(); err != nil /* .Start(command); err != nil */ {
		return "", decorate(err, "Failed to run command through remote shell", command)
	}
	if err = sess.Wait(); err != nil {
		return "", decorate(err, "Failed to finish command", command)
	}
	combined := strings.Join([]string{b.String(), e.String()}, "")
	return combined, nil
}

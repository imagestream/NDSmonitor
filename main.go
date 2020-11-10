/*
Author: Joshua Snyder
Date:   11-6-2020
Desc:   This program scrapes the ndsctl command and stores the results in a
        influxdb database.
*/
package main

import (
	"fmt"
	"log"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
)

const version string = "0.01"
const privatesshkey string = "/etc/NDSmonitor/id_rsa"

type config struct {
	username    string
	password    string
	ndshostname string
	ndsip       net.IP
	sshkey      bool
}

// Get values from the Enviroment and set the par
func startup() config {
	var s config

	log.Printf("NDS Monitor version: %s Starting up. \n", version)

	// Check for the remote host being set
	if os.Getenv("NDShost") == "" {
		log.Fatal("No NDS Host specified")
	} else {
		s.ndshostname = fmt.Sprintf("%s:22", os.Getenv("NDShost"))
	}

	// Check for the ssh username
	if os.Getenv("username") == "" {
		log.Fatal("No ssh username specified")
	} else {
		s.username = os.Getenv("username")
	}

	// Check for a password if it's not set then we check to see if the SSH key file is present.
	if os.Getenv("password") == "" {
		if fileExists(privatesshkey) {
			s.sshkey = true
		} else {
			log.Fatal("No password set or SSH key present")
		}
	} else {
		s.password = os.Getenv("password")
	}

	return s
}

// Verify that a file is present.
func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

// Connect to the server
func connectToHost(user, password, host string) (*ssh.Client, *ssh.Session, error) {
	sshConfig := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{ssh.Password(password)},
	}

	sshConfig.HostKeyCallback = ssh.InsecureIgnoreHostKey()

	client, err := ssh.Dial("tcp", host, sshConfig)
	if err != nil {
		return nil, nil, err
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, nil, err
	}

	return client, session, nil
}

/*
Commenting this out for now. I need to figure-out how to do SSH keys.
------
func publicKey(path string) (ssh.AuthMethod, error) {
	key, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		panic(err)
	}
	return ssh.ParsePublicKey(signer)
}

func connectSSH(s config) conn {
	config := &ssh.ClientConfig{
		User: s.username,
		Auth: []ssh.AuthMethod{
			publicKey(privatesshkey)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	conn, err := ssh.Dial("tcp", s.ndshostname, config)
	defer conn.Close()

	return conn
}
*/

func main() {
	settings := startup()
	fmt.Println("Startup Successful! Beginning monitoring loop")

	fmt.Printf("Going to use username: %s\n", settings.username)
	if settings.sshkey {
		fmt.Println("Using SSH Private Key")
	} else {
		fmt.Printf("Using this password: %s\n", settings.password)
	}

	client, session, err := connectToHost(settings.username, settings.password, settings.ndshostname)
	if err != nil {
		log.Fatal(err)
	}

	output, err := session.CombinedOutput("ndsctl json")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(output))
	client.Close()

}

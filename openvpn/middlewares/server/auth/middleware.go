package auth

import (
	"fmt"
	log "github.com/cihub/seelog"
	"github.com/mysterium/node/openvpn"
	"net"
	"regexp"
	"strconv"
)

// CredentialsChecker callback checks given auth primitives (i.e. customer identity signature / node's sessionId)
type CredentialsChecker func(username, password string) (bool, error)

type middleware struct {
	checkCredentials CredentialsChecker
	connection       net.Conn
	lastUsername     string
	lastPassword     string
	clientID         int
	keyID            int
	state            openvpn.State
}

// NewMiddleware creates server user_auth challenge authentication middleware
func NewMiddleware(credentialsChecker CredentialsChecker) *middleware {
	return &middleware{
		checkCredentials: credentialsChecker,
		connection:       nil,
	}
}

func (m *middleware) Start(connection net.Conn) error {
	m.connection = connection

	_, err := m.connection.Write([]byte("state on\n"))
	return err
}

func (m *middleware) Stop() error {
	_, err := m.connection.Write([]byte("state off\n"))
	return err
}

func (m *middleware) checkReAuth(line string) (cont bool, consumed bool, err error) {

	rule, err := regexp.Compile("^>CLIENT:REAUTH,(\\d),(\\d)$")
	if err != nil {
		return false, false, err
	}

	match := rule.FindStringSubmatch(line)
	if len(match) > 0 {
		m.Reset()
		m.state = openvpn.STATE_AUTH
		m.clientID, err = strconv.Atoi(match[1])
		m.keyID, err = strconv.Atoi(match[2])
		return false, true, nil
	}
	return true, false, nil
}

func (m *middleware) checkConnect(line string) (cont bool, consumed bool, err error) {

	rule, err := regexp.Compile("^>CLIENT:CONNECT,(\\d),(\\d)$")
	if err != nil {
		return false, false, err
	}

	match := rule.FindStringSubmatch(line)
	if len(match) > 0 {
		m.Reset()
		m.state = openvpn.STATE_AUTH
		m.clientID, err = strconv.Atoi(match[1])
		m.keyID, err = strconv.Atoi(match[2])
		return false, true, nil
	}

	return true, false, nil
}

func (m *middleware) checkPassword(line string) (cont bool, consumed bool, err error) {

	rule, err := regexp.Compile("^>CLIENT:ENV,password=(.*)$")
	if err != nil {
		return false, false, err
	}

	match := rule.FindStringSubmatch(line)
	if len(match) > 0 {
		if m.clientID < 0 {
			return false, false, fmt.Errorf("wrong auth state, no client id")
		}
		m.lastPassword = match[1]
		return false, true, nil
	}

	return true, false, nil
}

func (m *middleware) checkUsername(line string) (cont bool, consumed bool, err error) {

	rule, err := regexp.Compile("^>CLIENT:ENV,username=(.*)$")
	if err != nil {
		return false, false, err
	}

	match := rule.FindStringSubmatch(line)
	if len(match) > 0 {
		if m.clientID < 0 {
			return false, false, fmt.Errorf("wrong auth state, no client id")
		}
		m.lastUsername = match[1]
		return false, true, nil
	}

	return true, false, nil
}

func (m *middleware) checkEnvEnd(line string) (cont bool, consumed bool, err error) {

	rule, err := regexp.Compile("^>CLIENT:ENV,END$")
	if err != nil {
		return false, false, err
	}

	match := rule.FindStringSubmatch(line)
	if len(match) > 0 {
		return false, true, nil
	}

	return true, false, nil
}

func (m *middleware) ConsumeLine(line string) (consumed bool, err error) {
	if cont, consumed, err := m.checkReAuth(line); !cont {
		return consumed, err
	}

	if cont, consumed, err := m.checkConnect(line); !cont {
		return consumed, err
	}

	// further proceed only if in AUTH state
	if m.state != openvpn.STATE_AUTH {
		return false, nil
	}

	if cont, consumed, err := m.checkUsername(line); !cont {
		return consumed, err
	}

	if cont, consumed, err := m.checkPassword(line); !cont {
		return consumed, err
	}

	if cont, consumed, err := m.checkEnvEnd(line); !cont {
		if consumed {
			return m.authenticateClient()
		}
		return consumed, err
	}

	return false, err
}

func (m *middleware) authenticateClient() (consumed bool, err error) {
	m.state = openvpn.STATE_UNDEFINED

	if m.lastUsername == "" || m.lastPassword == "" {
		denyClientAuthWithMessage(m.connection, m.clientID, m.keyID, "missing username or password")
		return true, nil
	}

	log.Info("authenticating user: ", m.lastUsername, " clientID: ", m.clientID, " keyID: ", m.keyID)

	authenticated, err := m.checkCredentials(m.lastUsername, m.lastPassword)
	if err != nil {
		log.Error("Authentication error: ", err)
		denyClientAuthWithMessage(m.connection, m.clientID, m.keyID, "internal error")
		return true, nil
	}

	if authenticated {
		approveClient(m.connection, m.clientID, m.keyID)
	} else {
		denyClientAuthWithMessage(m.connection, m.clientID, m.keyID, "wrong username or password")
	}
	return true, nil
}

func approveClient(conn net.Conn, clientID, keyID int) {
	writeStringToConn(conn, "client-auth-nt "+strconv.Itoa(clientID)+" "+strconv.Itoa(keyID)+"\n")
}

func denyClientAuthWithMessage(conn net.Conn, clientID, keyID int, message string) {
	writeStringToConn(conn, "client-deny "+strconv.Itoa(clientID)+" "+strconv.Itoa(keyID)+" "+message+"\n")
}

func writeStringToConn(conn net.Conn, message string) {
	_, err := conn.Write([]byte(message + "\n"))
	if err != nil {
		log.Error("Management communication error: ", err)
	}
}

func (m *middleware) Reset() {
	m.lastUsername = ""
	m.lastPassword = ""
	m.clientID = -1
	m.keyID = -1
	m.state = openvpn.STATE_UNDEFINED
}

// Package protocol defines the wire protocol used between the Echo chat
// client and server. All messages are newline-delimited UTF-8 text.
//
// Client → Server commands:
//
//	NICK <username>
//	CREATE <room>
//	JOIN <room>
//	LEAVE
//	MSG <text>
//	LIST
//	QUIT
//
// Server → Client responses:
//
//	OK <detail>
//	ERR <detail>
//	BROADCAST <formatted message>
//	HISTORY <formatted message>
//	SYS <info message>
package protocol

import (
	"fmt"
	"strings"
)

// ── Client command verbs ────────────────────────────────────────────────────

const (
	CmdNick   = "NICK"
	CmdCreate = "CREATE"
	CmdJoin   = "JOIN"
	CmdLeave  = "LEAVE"
	CmdMsg    = "MSG"
	CmdList   = "LIST"
	CmdQuit   = "QUIT"
)

// ── Server response prefixes ────────────────────────────────────────────────

const (
	RespOK        = "OK"
	RespErr       = "ERR"
	RespBroadcast = "BROADCAST"
	RespHistory   = "HISTORY"
	RespSys       = "SYS"
)

// ParseCommand splits a raw line into the verb and its argument (if any).
// For example "MSG hello world" → ("MSG", "hello world").
func ParseCommand(line string) (verb, arg string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", ""
	}
	parts := strings.SplitN(line, " ", 2)
	verb = strings.ToUpper(parts[0])
	if len(parts) > 1 {
		arg = parts[1]
	}
	return verb, arg
}

// Encode formats a server response as "PREFIX payload\n".
func Encode(prefix, payload string) string {
	if payload == "" {
		return prefix + "\n"
	}
	return fmt.Sprintf("%s %s\n", prefix, payload)
}

// FormatBroadcast returns the canonical broadcast format:
//
//	[room][user]: message
func FormatBroadcast(room, user, message string) string {
	return fmt.Sprintf("[%s][%s]: %s", room, user, message)
}

// FormatSystem returns a system-level notification:
//
//	*** message
func FormatSystem(message string) string {
	return fmt.Sprintf("*** %s", message)
}

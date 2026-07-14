package main

import (
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbletea"
)

const (
	kittyKeyModShift = 1 << iota
	kittyKeyModAlt
	kittyKeyModCtrl
	kittyKeyModSuper
)

type kittyKeyEvent struct {
	Code      rune
	Modifiers int
	EventType int
}

// parseKittyKeyEvent extracts CSI-u keyboard events that Bubble Tea 1.x
// reports as its private unknown-CSI message type. Reflection is limited to
// byte-slice messages and the sequence is validated before it is accepted.
func parseKittyKeyEvent(msg tea.Msg) (kittyKeyEvent, bool) {
	value := reflect.ValueOf(msg)
	if !value.IsValid() || value.Kind() != reflect.Slice || value.Type().Elem().Kind() != reflect.Uint8 {
		return kittyKeyEvent{}, false
	}

	raw := append([]byte(nil), value.Bytes()...)
	if len(raw) < 4 || raw[0] != '\x1b' || raw[1] != '[' || raw[len(raw)-1] != 'u' {
		return kittyKeyEvent{}, false
	}

	params := strings.Split(string(raw[2:len(raw)-1]), ";")
	keyParts := strings.Split(params[0], ":")
	code, err := strconv.Atoi(keyParts[0])
	if err != nil || code < 0 || code > utf8.MaxRune {
		return kittyKeyEvent{}, false
	}

	encodedModifiers := 1
	eventType := 1
	if len(params) > 1 && params[1] != "" {
		modifierParts := strings.Split(params[1], ":")
		encodedModifiers, err = strconv.Atoi(modifierParts[0])
		if err != nil || encodedModifiers < 1 {
			return kittyKeyEvent{}, false
		}
		if len(modifierParts) > 1 {
			eventType, err = strconv.Atoi(modifierParts[1])
			if err != nil {
				return kittyKeyEvent{}, false
			}
		}
	}

	return kittyKeyEvent{
		Code:      rune(code), // #nosec G115 -- code is bounded to utf8.MaxRune above.
		Modifiers: encodedModifiers - 1,
		EventType: eventType,
	}, true
}

func (k kittyKeyEvent) isShiftEnter() bool {
	return k.EventType == 1 && (k.Code == '\r' || k.Code == '\n') && k.Modifiers&kittyKeyModShift != 0
}

func (k kittyKeyEvent) isCommandImportant() bool {
	if k.EventType != 1 || k.Modifiers&kittyKeyModSuper == 0 {
		return false
	}
	return k.Code == '/'
}

func (k kittyKeyEvent) bubbleTeaKeyMsg() (tea.KeyMsg, bool) {
	if k.EventType != 1 || k.Modifiers&kittyKeyModSuper != 0 {
		return tea.KeyMsg{}, false
	}
	alt := k.Modifiers&kittyKeyModAlt != 0
	switch k.Code {
	case '\x1b':
		return tea.KeyMsg{Type: tea.KeyEsc, Alt: alt}, true
	case '\r', '\n':
		return tea.KeyMsg{Type: tea.KeyEnter, Alt: alt}, true
	case '\t':
		if k.Modifiers&kittyKeyModShift != 0 {
			return tea.KeyMsg{Type: tea.KeyShiftTab, Alt: alt}, true
		}
		return tea.KeyMsg{Type: tea.KeyTab, Alt: alt}, true
	case '\b', '\x7f':
		return tea.KeyMsg{Type: tea.KeyBackspace, Alt: alt}, true
	}

	if k.Modifiers&kittyKeyModCtrl != 0 && k.Code >= ' ' && k.Code <= '~' {
		code := k.Code
		if code >= 'A' && code <= 'Z' {
			code += 'a' - 'A'
		}
		return tea.KeyMsg{Type: tea.KeyType(code & 0x1f), Alt: alt}, true
	}
	if k.Code > 0 && k.Code <= 0x10ffff {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k.Code}, Alt: alt}, true
	}
	return tea.KeyMsg{}, false
}

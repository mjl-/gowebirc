package irc

import (
	"fmt"
	"net"
	"bufio"
	"errors"
	"strings"
	"strconv"
)

const (
	RPLwelcome		= 1
	RPLaway			= 301
	RPLchannelmode		= 324
	RPLchannelmodechanged	= 329
	RPLtopic		= 332
	RPLtopicset		= 333
	RPLinviting		= 341
	RPLnames		= 353
	RPLnamesdone		= 366
	RPLnickinuse		= 433

	RPLwhoisuser		= 311
	RPLwhoisserver		= 312
	RPLwhoisoperator	= 313
	RPLwhoisidle		= 317
	RPLendofwhois		= 318
	RPLwhoischannels	= 319

	RPLnosuchnick		= 401
	RPLnosuchchannel	= 403
	RPLcannotsendtochan	= 404
	RPLtoomanychannels	= 405
	RPLwasnosuchnick	= 406
)

var desttypes []int

func init() {
	desttypes = []int{301, 311, 312, 313, 317, 318, 319,      // nick as first token
	324, 325, 329, 331, 332, 333, 341, 346, 347, 348, 349, 366, 367, 368,   // channel as first token
	401, 403, 404, 405, 406, 467, 471, 473, 474, 475, 476, 477, 478, 482,   // nick or channel as first token
	}
}


const (
	Maximsglen		= 512
)

type From struct {
	Nick, Server, User, Host string
}

func (f From) GoString() string {
	return fmt.Sprintf("From(nick=%#v, server=%#v, user=%#v, host=%#v)", f.Nick, f.Server, f.User, f.Host)
}

func (f From) Text() string {
	if f.Host == "" || f.User == "" {
		return f.Server
	}
        return fmt.Sprintf("%s!%s@%s", f.Nick, f.User, f.Host)
}

func fromparse(s string) *From {
	l := strings.SplitN(s, "@", 2)
	if len(l) != 2 {
		return &From{
			Nick: s,
			Server: s,
		}
	}

	host := l[1]
	s = l[0]
	l = strings.SplitN(s, "!", 2)
	if len(l) != 2 {
		return &From{
			Host: host,
			Nick: s,
		}
	}
	return &From{
		Host: host,
		Nick: l[0],
		User: l[1],
	}
}


type TPass struct { Pass string }
type TNick struct { Name string }
type TUser struct { Name string }
type TNames struct { Name string }
type TPrivmsg struct { Who, M string }
type TNotice struct { Who, M string }
type TJoin struct { Where, Key string }
type TAway struct { M string }
type TPart struct { Where string }
type TTopicget struct { Where string }
type TTopicset struct { Where, M string }
type TQuit struct { M string }
type TPong struct { Who, M string }
type TMode struct {
	Where string
	Modes []string // for sending, we do not split between mode and modeargs
}
type TKick struct { Where, Who, M string }
type TWhois struct { Name string }
type TInvite struct { Who, Where string }
type TPing struct { Server string }

type Tmsger interface {
	Packtmsg() string
}

func (m TPass) Packtmsg() string { return fmt.Sprintf("PASS %s\r\n", m.Pass) }
func (m TNick) Packtmsg() string { return fmt.Sprintf("NICK %s\r\n", m.Name) }
func (m TUser) Packtmsg() string { return fmt.Sprintf("USER none 0 * :%s\r\n", m.Name) }
func (m TNames) Packtmsg() string { return fmt.Sprintf("NAMES %s\r\n", m.Name) }
func (m TPrivmsg) Packtmsg() string { return fmt.Sprintf("PRIVMSG %s :%s\r\n", m.Who, m.M) }
func (m TNotice) Packtmsg() string { return fmt.Sprintf("NOTICE %s :%s\r\n", m.Who, m.M) }
func (m TJoin) Packtmsg() string {
	r := fmt.Sprintf("JOIN %s", m.Where)
	if m.Key != "" {
		r += " "+m.Key
	}
	r += "\r\n"
	return r
}
func (m TAway) Packtmsg() string { return fmt.Sprintf("AWAY :%s\r\n", m.M) }
func (m TPart) Packtmsg() string { return fmt.Sprintf("PART %s\r\n", m.Where) }
func (m TTopicget) Packtmsg() string { return fmt.Sprintf("TOPIC %s\r\n", m.Where) }
func (m TTopicset) Packtmsg() string { return fmt.Sprintf("TOPIC %s :%s\r\n", m.Where, m.M) }
func (m TQuit) Packtmsg() string { return fmt.Sprintf("QUIT :%s\r\n", m.M) }
func (m TPong) Packtmsg() string { return fmt.Sprintf("PONG %s %s\r\n", m.Who, m.M) }
func (m TMode) Packtmsg() string {
	r := fmt.Sprintf("MODE %s", m.Where)
	for _, mode := range m.Modes {
		r += " "+mode
	}
	r += "\r\n"
	return r
}
func (m TKick) Packtmsg() string { return fmt.Sprintf("KICK %s %s %s\r\n", m.Where, m.Who, m.M) }
func (m TWhois) Packtmsg() string { return fmt.Sprintf("WHOIS %s\r\n", m.Name) }
func (m TInvite) Packtmsg() string { return fmt.Sprintf("INVITE %s %s\r\n", m.Who, m.Where) }
func (m TPing) Packtmsg() string { return fmt.Sprintf("PING %s\r\n", m.Server) }


type Irccon struct {
	Dialaddr string
	Nick, Lnick string
	Server string

	c net.Conn
	r *bufio.Reader
}

func MakeIrccon(addr string, conn net.Conn) *Irccon {
	ic := &Irccon{addr, "", "", "", conn, bufio.NewReader(conn)}
	return ic
}

type Rmsger interface {
}

type RNick struct {
	line string
	From *From
	Cmd, Name string
}

type Mode struct {
	Mode string
	Params []string
}

type RMode struct {
	line string
	From *From
	Cmd string
	Where string
	Modes []Mode
}

type RQuit struct {
	line string
	From *From
	Cmd string
	Msg string
}

type RError struct {
	line string
	From *From
	Cmd string
	Msg string
}

type RSquit struct {
	line string
	From *From
	Cmd string
	Msg string
}

type RJoin struct {
	line string
	From *From
	Cmd string
	Where string
}

type RPart struct {
	line string
	From *From
	Cmd string
	Where string
	Msg string
}

type RTopic struct {
	line string
	From *From
	Cmd string
	Where string
	Msg string
}

type RPrivmsg struct {
	line string
	From *From
	Cmd string
	Where string
	Msg string
}

type RNotice struct {
	line string
	From *From
	Cmd string
	Where string
	Msg string
}

type RPing struct {
	line string
	From *From
	Cmd string
	Who string
	Msg string
}

type RPong struct {
	line string
	From *From
	Cmd string
	Who string
	Msg string
}

type RKick struct {
	line string
	From *From
	Cmd string
	Where, Who, Msg string
}

type RInvite struct {
	line string
	From *From
	Cmd string
	Who, Where string
}

type RUnknown struct {
	line string
	From *From
	Cmd string
	Where string // may be empty
	Params []string
}

type RReplytext struct {
	line string
	From *From
	Cmd string
	Where string // may be empty
	Params []string
}

type RErrortext struct {
	line string
	From *From
	Cmd string
	Where string // may be empty
	Params []string
}


func (i *Irccon) Close() {
	i.c.Close()
}

func (i *Irccon) SetNick(nick string) {
	i.Nick = nick
	i.Lnick = Lower(nick)
}

func (i *Irccon) Readrmsg() (m Rmsger, err error) {
	s, err := i.r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	s = s[:len(s)-1]
	if s != "" && s[len(s)-1] == '\r' {
		s = s[:len(s)-1]
	}
	line := s

	var f *From
	if strings.HasPrefix(s, ":") {
		l := strings.SplitN(s, " ", 2)
		if len(l) != 2 || l[1] == "" {
			return nil, errors.New("bad line, bogus prefix")
		}
		f = fromparse(l[0][1:])
		s = l[1]
	}

	l := strings.SplitN(s, " ", 2)
	if len(l) != 2 {
		return nil, errors.New("bad line, missing command")
	}
	cmd := l[0]
	switch cmd {
	case "NICK", "MODE", "QUIT", "JOIN", "PART", "TOPIC", "KICK":
		if f == nil {
			return nil, fmt.Errorf("bad line, prefix required for %#v but missing", cmd)
		}
	}
	s = l[1]
	params := []string{}
	for s != "" {
		if strings.HasPrefix(s, ":") {
			params = append(params, s[1:])
			break
		}
		l = strings.SplitN(s, " ", 2)
		params = append(params, l[0])
		s = ""
		if len(l) > 1 {
			s = l[1]
		}
	}

	switch cmd {
	case "NICK":
		if len(params) != 1 {
			return nil, errors.New("bad nick message, need 1 param")
		}
		return RNick{line, f, cmd, params[0]}, nil

	case "MODE":
		if len(params) < 2 {
			return nil, errors.New("bad mode message, need at least two params")
		}
		i := 1

		modes := []Mode{}
		for i < len(params) {
			p := params[i]
			if !strings.HasPrefix(params[i], "-") && !strings.HasPrefix(params[i], "+") {
				return nil, errors.New("bad params for mode")
			}
			mode := p
			i++
			modeparams := []string{}
			for i < len(params) && !strings.HasPrefix(params[i], "-") && !strings.HasPrefix(params[i], "+") {
				modeparams = append(modeparams, params[i])
				i++
			}
			modes = append(modes, Mode{mode, modeparams})
		}
		return RMode{line, f, cmd, params[0], modes}, nil

	// xxx more commands
	case "QUIT", "ERROR", "SQUIT":
		if len(params) > 1 {
			return nil, errors.New("bad params for quit/error/squit")
		}
		s := ""
		if len(params) == 1 {
			s = params[0]
		}
		switch cmd {
		case "QUIT":
			return RQuit{line, f, cmd, s}, nil
		case "ERROR":
			return RError{line, f, cmd, s}, nil
		case "SQUIT":
			return RSquit{line, f, cmd, s}, nil
		}

	case "JOIN":
		if len(params) != 1 {
			return nil, errors.New("bad params for join")
		}
		return RJoin{line, f, cmd, params[0]}, nil

	case "PART", "TOPIC", "PRIVMSG", "NOTICE":
		m := ""
		switch len(params) {
		case 1:
		case 2:
			m = params[1]
		default:
			return nil, errors.New("bad params for part")
		}
		switch cmd {
		case "PART":
			return RPart{line, f, cmd, params[0], m}, nil
		case "TOPIC":
			return RTopic{line, f, cmd, params[0], m}, nil
		case "PRIVMSG":
			return RPrivmsg{line, f, cmd, params[0], m}, nil
		case "NOTICE":
			return RPart{line, f, cmd, params[0], m}, nil
		}

	case "PING":
		switch len(params) {
		case 1:
			return RPing{line, f, cmd, params[0], ""}, nil
		case 2:
			return RPing{line, f, cmd, params[0], params[1]}, nil
		default:
			return nil, errors.New("bad params for ping")
		}

	case "PONG":
		switch len(params) {
		case 1:
			return RPong{line, f, cmd, params[0], ""}, nil
		case 2:
			return RPong{line, f, cmd, params[0], params[1]}, nil
		default:
			return nil, errors.New("bad params for pong")
		}

	case "KICK":
		switch len(params) {
		case 2:
			return RKick{line, f, cmd, params[0], params[1], ""}, nil
		case 3:
			return RKick{line, f, cmd, params[0], params[1], params[2]}, nil
		default:
			return nil, errors.New("bad params for kick")
		}

	case "INVITE":
		if len(params) != 2 {
			return nil, errors.New("bad params for invite")
		}
		return RInvite{line, f, cmd, params[0], params[1]}, nil

	default:
		if len(cmd) != 3 || strings.Trim(cmd, "0123456789") != "" {
			return RUnknown{line, f, cmd, "", params}, nil
		}

		where := ""
		id, _ := strconv.Atoi(cmd)
		if len(params) >= 1 {
			for _, xid := range desttypes {
				if id == xid {
					where = params[1]
					params = params[2:]
					break
				}
			}
		}
		if id == 353 && len(params) >= 3 {
			return RReplytext{line, f, cmd, params[2], params[1:]}, nil
		}
		if where == "" {
			params = params[1:]
		}
		if cmd[0] == '4' || cmd[0] == '5' {
			return RErrortext{line, f, cmd, where, params}, nil
		}
		return RReplytext{line, f, cmd, where, params}, nil
	}
	return nil, fmt.Errorf("unrecognized message %#v", cmd)
}

func (i *Irccon) Writemsg(t Tmsger) (err error) {
	s := t.Packtmsg()
	buf := []byte(s)
	_, err = i.c.Write(buf)
	fmt.Println("tmsg written...")
	return err
}

func (i *Irccon) IsSelf(nick string) bool {
	return i.Nick == Lower(nick)
}

func Lower(s string) string {
	r := ""
	for _, c := range s {
		switch {
		case c >= 'A' && c <= 'Z':
			c += 'a'-'A'
		case c == '[':
			c = '{'
		case c == ']':
			c = '}'
		case c == '\\':
			c = '|'
		case c == '~':
			c = '^'
		}
		r += string(c)
	}
	return r
}

func IsChannel(s string) bool {
	for _, c := range s {
		fmt.Println(c)
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			return false
		}
		for _, x := range "[]^_`{|}" {
			if c == x {
				return false
			}
		}
		return true // we only look at the first char
	}
	return false // empty string
}

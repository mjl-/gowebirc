/*
to run:
cd $HOME/code/gowebirc/ && GOPATH=$PWD go run src/gowebirc.go
*/

package main

import (
	"flag"
	"encoding/json"
	"encoding/base64"
	"os/user"
	"fmt"
	"irc"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Event interface {
	eid()	int64
	isState()	bool
}
type EventMsg struct {
	T                  string
	Eid, Sid, Uu, Time int64
	Who, Text          string
}
func (e EventMsg) eid() int64 {
	return e.Eid
}
func (e EventMsg) isState() bool {
	return false
}

type EventAction struct {
	T                  string
	Eid, Sid, Uu, Time int64
	Text          string
}
func (e EventAction) eid() int64 {
	return e.Eid
}
func (e EventAction) isState() bool {
	return false
}

type EventStatus struct {
	T                  string
	Eid, Sid, Uu, Time int64
	Text               string
}
func (e EventStatus) eid() int64 {
	return e.Eid
}
func (e EventStatus) isState() bool {
	return false
}

type EventClear struct {
	T   string
	Eid int64
}
func (e EventClear) eid() int64 {
	return e.Eid
}
func (e EventClear) isState() bool {
	return true
}

type EventCon struct {
	T	string
	Eid, Sid	int64
	Status	string		// connecting, connected, disconnected
	Nick	string
	Irclagms	int		// in ms
}
func (e EventCon) eid() int64 {
	return e.Eid
}
func (e EventCon) isState() bool {
	return true
}

type EventSession struct {
	T        string
	Eid, Sid int64
	Name     string
	S0	 int64
	IsChan	bool
}
func (e EventSession) eid() int64 {
	return e.Eid
}
func (e EventSession) isState() bool {
	return true
}

type EventSessionname struct {
	T	string
	Eid, Sid	int64
	Name	string
}
func (e EventSessionname) eid() int64 {
	return e.Eid
}
func (e EventSessionname) isState() bool {
	return true
}

type EventRemove struct {
	T        string
	Eid, Sid int64
}
func (e EventRemove) eid() int64 {
	return e.Eid
}
func (e EventRemove) isState() bool {
	return true
}

type EventUsers struct {
	T	string
	Eid, Sid int64
	Reset	bool
	Add	[]string
	Remove	[]string
}
func (e EventUsers) eid() int64 {
	return e.Eid
}
func (e EventUsers) isState() bool {
	return true
}


type (
	Post struct {
		sid, u int64
		line   string
	}

	Eventwait struct {
		evl    []Event
		lastid int64
	}
	Get struct {
		ewc    chan Eventwait
		lastid int64
	}
	Eventwaiter struct {
		ewc    chan Eventwait
		lastid int64
	}

	Connection struct {
		ic       *irc.Irccon		// initially not present!
		writes   []*irc.Tmsger		// pending messages, to write to irc server
		writereq	chan []*irc.Tmsger	// channel if our irc writer proc is ready to write
		s0		*Session
		sessions	map[string]*Session	// sessions by name (channel or user)
		status		string			// connecting, connected, disconnected
		addr		string
		nick		string
		dead		bool		// whether this connection is dead or has been replaced by a new one
	}

	Session struct {
		sid     int64
		name    string
		cantalk bool
		is0	bool
		isChan	bool
		c       *Connection
	}

	Ircs struct {
		s   *Session
		c   *Connection
		ic	*irc.Irccon
		err string
	}
	Ircw struct {
		c  *Connection
		rc chan []*irc.Tmsger
	}
	Ircr struct {
		c *Connection
		r irc.Rmsger
	}
)

func (s Session) s0sid() int64 {
	if s.c != nil {
		return s.c.s0.sid
	}
	return 0
}

var (
	getc  chan Get
	postc chan Post

	ircrc chan Ircr
	ircwc chan Ircw
	ircsc chan Ircs

	httpauth string
	defaultNick *string
)

func post(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}

	defer func() {
		if r := recover(); r != nil {
			w.WriteHeader(400)
			fmt.Print(r)
		}
	}()

	l := r.FormValue("line")
	sid, _ := strconv.ParseInt(r.FormValue("sid"), 10, 0)
	u, _ := strconv.ParseInt(r.FormValue("u"), 10, 0)

	postc <- Post{sid, u, l}

	d := map[string]string{"r": "ok"}
	rr, err := json.Marshal(d)
	if err != nil {
		panic(fmt.Sprintf("bad marshal: %v", err))
	}
	w.Write(rr)
}

func get(w http.ResponseWriter, r *http.Request) {
	f, ok := w.(http.Flusher)
	if !ok {
		log.Fatal("flushing http responses not supported")
	}

	h := w.Header()
	h.Set("content-type", "text/event-stream")
	h.Set("cache-control", "no-cache, max-age=0")
	w.WriteHeader(200)

	fmt.Fprint(w, "retry: 1000\n")
	f.Flush()

	s := r.FormValue("Last-Event-ID")
	lasteventid := int64(-1)
	if s != "" {
		lasteventid, _ = strconv.ParseInt(s, 10, 0)
	}
	ewc := make(chan Eventwait, 1)

	sse := func(evl []Event, lastid int64) {
		v, err := json.Marshal(evl)
		if err != nil {
			log.Fatal("could not marshal to json: " + err.Error())
		}
		fmt.Fprintf(w, "id: %d\n\n", lastid)
		fmt.Fprintf(w, "data: %s\n\n", v)
		f.Flush()
		fmt.Println("wrote event", string(v), evl, lastid)
	}

	for {
		getc <- Get{ewc, lasteventid}
		ew := <-ewc
		sse(ew.evl, ew.lastid)
		lasteventid = ew.lastid
	}
}

func newIrc(addr, nick string, s *Session, c *Connection) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		ircsc <- Ircs{s, c, nil, fmt.Sprintf("connect: %s", err)}
		return
	}
	fmt.Println("connected to irc server")

	ic := irc.MakeIrccon(addr, conn)
	err = ic.Writemsg(irc.TNick{nick})
	if err == nil {
		err = ic.Writemsg(irc.TUser{nick})
	}
	if err != nil {
		log.Fatal(fmt.Sprintf("error writing nick & user commands: %s", err))
	}
	ic.SetNick(nick)
	fmt.Println("wrote nick & user commands to server")

	ircsc <- Ircs{s, c, ic, ""}

	go func() {
		for {
			rm, err := ic.Readrmsg()
			if err != nil {
				ircsc <- Ircs{s, c, nil, fmt.Sprintf("error reading irc msg: %s", err)}
				return
			}
			fmt.Printf("<<< %v\n", rm)
			ircrc <- Ircr{c, rm}
		}
	}()
	go func() {
		rc := make(chan []*irc.Tmsger, 1)
		for {
			ircwc <- Ircw{c, rc}
			msgs := <-rc
			if msgs == nil {
				return
			}
			for _, t := range msgs {
				fmt.Printf(">>> %s", (*t).Packtmsg())
				err := ic.Writemsg(*t)
				if err != nil {
					ircsc <- Ircs{s, c, nil, fmt.Sprintf("write error: %s", err.Error())}
					return
				}
			}
		}
	}()
}

func (c *Connection) ircWrite(t irc.Tmsger) {
	fmt.Println("request to write irc message", t)
	c.writes = append(c.writes, &t)
	c.ircKick()
}

func (c *Connection) ircWriterQueue(wc chan []*irc.Tmsger) {
	c.writereq = wc
	c.ircKick()
}

func (c *Connection) ircKick() {
	if c.writereq == nil || len(c.writes) == 0 {
		return
	}
	c.writereq <- c.writes
	c.writes = nil
}

func now() int64 {
	return time.Now().Unix()
}

func srv() {
	eventhist := []Event{}
	eventwaiters := make([]Eventwaiter, 0)

	eidgen := int64(0)
	eidNext := func() int64 {
		eidgen += 1
		return eidgen
	}

	sidgen := int64(0)
	sidNext := func() int64 {
		sidgen += 1
		return sidgen
	}

	sessions := map[int64]*Session{
		int64(0): {int64(0), "0", false, true, false, nil},
	}

	makeInitEvents := func() []Event {
		l := make([]Event, 0)
		l = append(l, EventClear{"clear", 0})
		for _, s := range sessions {
			if s.sid == 0 {
				continue
			}
			if s.is0 && s.c != nil {
				nick := "a gopher"
				if s.c.ic != nil {
					nick = s.c.ic.Nick
				}
				l = append(l, EventCon{"con", 0, s.sid, s.c.status, nick, 0})
			}
			l = append(l, EventSession{"session", 0, s.sid, s.name, s.s0sid(), s.isChan})
		}
		return l
	}

	eventKick := func() {
		fmt.Printf("eventKick, %d waiters, eventhist len %d\n", len(eventwaiters), len(eventhist))
		n := make([]Eventwaiter, 0)
		for _, eventwaiter := range eventwaiters {
			fmt.Printf("len eventhist %v, lastid %v\n", len(eventhist), eventwaiter.lastid)
			o := len(eventhist)
			for ; o > 0; o-- {
				fmt.Println("eventid", eventhist[o-1], eventhist[o-1].eid())
				if eventhist[o-1].eid() <= eventwaiter.lastid {
					break
				}
			}
			fmt.Printf("eventKick, o %d\n", o)
			if o < len(eventhist) {
				evl := eventhist[o:]
				eventwaiter.ewc <- Eventwait{evl, evl[len(evl)-1].eid()}
				fmt.Println("unqueued an eventwaiter...")
			} else {
				n = append(n, eventwaiter)
			}
		}
		eventwaiters = n
	}

	eventAdd := func(e Event) {
		fmt.Println("event added", e)
		eventhist = append(eventhist, e)
		eventKick()
	}

	// ensure we always have 1 event to send to frontend
	eventAdd(EventStatus{"status", eidNext(), int64(0), int64(0), now(), "gowebirc started"})

	sessionAdd := func(s *Session) {
		sessions[s.sid] = s
		fmt.Println("session added", s)
		eventAdd(EventSession{"session", eidNext(), s.sid, s.name, s.s0sid(), s.isChan})
		if s.c != nil {
			s.c.sessions[irc.Lower(s.name)] = s
		}
	}

	sessionEnsure := func(c *Connection, name string) *Session {
		s, ok := c.sessions[irc.Lower(name)]
		if !ok {
			s = &Session{sidNext(), name, true, false, irc.IsChannel(name), c}
			sessionAdd(s)
			eventAdd(EventSession{"session", eidNext(), s.sid, name, s.s0sid(), s.isChan})
		}
		return s
	}

	sessionStatus := func(ss *Session, s string) {
		fmt.Println("session status", ss, s)
		eventAdd(EventStatus{"status", eidNext(), ss.sid, int64(0), now(), s})
	}

	status := func(c *Connection, s string) {
		fmt.Println("status", s)
		eventAdd(EventStatus{"status", eidNext(), c.s0.sid, int64(0), now(), s})
	}

	statusAll := func(c *Connection, s string) {
		fmt.Println("statusAll", s)
		for _, ss := range c.sessions {
			eventAdd(EventStatus{"status", eidNext(), ss.sid, int64(0), now(), s})
		}
	}

	status0 := func(s string) {
		fmt.Println("status0", s)
		eventAdd(EventStatus{"status", eidNext(), sessions[0].sid, int64(0), now(), s})
	}


	for {
		select {
		case r := <-ircsc:
			// connection status
			fmt.Println("irc status change", r)

			c := r.c
			if r.err != "" {
				if !c.dead {
					sessionStatus(r.s, "disconnected: "+r.err)
					eventAdd(EventCon{"con", eidNext(), r.s.sid, "disconnected", "", 0})
					c.status = "disconnected"
					c.dead = true
					if c.ic != nil {
						c.ic.Close()
						c.ic = nil
					}
					fmt.Println("lost connection...")
				}
			} else {
				r.s.c = c
				if r.ic != nil {
					c.ic = r.ic
				}
				c.status = "connected"
				sessionStatus(r.s, "connected")
				eventAdd(EventCon{"con", eidNext(), r.s.sid, "connected", c.ic.Nick, 0})
			}

		case w := <-ircwc:
			// writer wants a write
			fmt.Println("writer wants a write")
			w.c.ircWriterQueue(w.rc)

		case get := <-getc:
			// request for an sse event
			fmt.Println("received request for events", get)

			// new connections that are not reconnects will have lastid -1 (set by us).
			// reconnects may start out with a lastid (given by browser), but it may be too old to be updated with events.  a new state will be sent in that case.

			n := len(eventhist)-int(get.lastid)-1
			if get.lastid >= 0 && n <= 0 {
				eventwaiters = append(eventwaiters, Eventwaiter{get.ewc, get.lastid})
				fmt.Println("scheduled get for later event")
			} else {
				events := make([]Event, 0)
				stateEvents := true
				if get.lastid < 0 || n > 10000 {
					events = append(events, makeInitEvents()...)
					stateEvents = false
				}
				newlastid := int64(0)
				for _, e := range eventhist[len(eventhist)-n:] {
					newlastid = e.eid()
					if stateEvents || !e.isState() {
						events = append(events, e)
					}
				}
				get.ewc <- Eventwait{events, newlastid}
				fmt.Println("sent events", events, newlastid)
			}

		case r := <-ircrc:
			// read an irc message
			fmt.Println("reader read an irc message", r.r)
			mm := r.r
			switch m := mm.(type) {
			case irc.RNick:
				if r.c.ic.IsSelf(m.From.Nick) {
					r.c.ic.SetNick(m.Name)
					statusAll(r.c, fmt.Sprintf("you (%s) are now know as %s", m.From.Text(), m.Name))
					r.c.nick = m.Name
					eventAdd(EventCon{"con", eidNext(), r.c.s0.sid, r.c.status, m.Name, 0})
				} else {
					// xxx figure out the sessions m.From.Nick is active in...
					status(r.c, fmt.Sprintf("%s (%s) is now known as %s", m.From.Nick, m.From.Text(), m.Name))
					lnick := irc.Lower(m.From.Nick)
					for name, ss := range r.c.sessions {
						if lnick == name {
							eventAdd(EventSessionname{"sessionname", eidNext(), ss.sid, m.From.Nick})
							delete(r.c.sessions, name)
							r.c.sessions[lnick] = ss
							break
						}
					}
				}
			case irc.RMode:
				modes := ""
				for _, m := range m.Modes {
					modes += " "+m.Mode
					for _, p := range m.Params {
						modes += " "+p
					}
				}
				msg := fmt.Sprintf("mode%s by %s (%s)", modes, m.From.Nick, m.From.Text())
				if r.c.ic.IsSelf(m.Where) {
					status(r.c, msg)
				} else {
					ss := sessionEnsure(r.c, m.Where)
					sessionStatus(ss, msg)
				}
			case irc.RQuit:
				if r.c.ic.IsSelf(m.From.Nick) {
					statusAll(r.c, fmt.Sprintf("you (%s) have quite from irc: %s", m.From.Text(), m.Msg))
					// xxx some more bookkeeping, such as tearing down connection
					continue
				}
				// xxx find all sessions where m.From.Nick is in...
				status(r.c, fmt.Sprintf("%s (%s) has quit from irc: %s", m.From.Nick, m.From.Text(), m.Msg))
			case irc.RError:
				statusAll(r.c, fmt.Sprintf("error from server: %s", m.Msg))
				// xxx tear down connection
			case irc.RSquit:
				statusAll(r.c, fmt.Sprintf("squit: %s", m.Msg))
			case irc.RJoin:
				ss := sessionEnsure(r.c, m.Where)
				sessionStatus(ss, fmt.Sprintf("%s (%s) joined", m.From.Nick, m.From.Text()))
				// xxx update list of joined users
			case irc.RPart:
				ss := sessionEnsure(r.c, m.Where)
				sessionStatus(ss, fmt.Sprintf("%s (%s) left: %s", m.From.Nick, m.From.Text(), m.Msg))
				// xxx update list of joined users
			case irc.RTopic:
				ss := sessionEnsure(r.c, m.Where)
				sessionStatus(ss, fmt.Sprintf("%s (%s) set new topic: %s", m.From.Nick, m.From.Text(), m.Msg))
			case irc.RPrivmsg:
				where := m.Where
				if r.c.ic.IsSelf(m.Where) && m.From.Nick != "" {
					where = m.From.Nick
				}
				nick := ""
				if m.From != nil{
					nick = m.From.Nick
				}
				ss := sessionEnsure(r.c, where)
				if strings.HasPrefix(m.Msg, "\u0001ACTION ") && strings.HasSuffix(m.Msg, "\u0001") {
					s := fmt.Sprintf("*%s %s", nick, m.Msg[len("\u0001ACTION "):len(m.Msg)-1])
					eventAdd(EventStatus{"status", eidNext(), ss.sid, 0, now(), s})
				} else {
					eventAdd(EventMsg{"msg", eidNext(), ss.sid, 0, now(), m.From.Nick, m.Msg})
				}
			case irc.RNotice:
				var ss *Session
				if irc.IsChannel(m.Where) {
					ss = r.c.sessions[irc.Lower(m.Where)]
				}
				if r.c.ic.IsSelf(m.Where) && m.From != nil {
					ss = r.c.sessions[irc.Lower(m.From.Nick)]
				}
				if ss == nil {
					ss = r.c.s0
				}
				from := ""
				if m.From != nil {
					from = m.From.Nick
				}
				sessionStatus(ss, fmt.Sprintf("%s: %s", from, m.Msg))
			case irc.RPing:
				fmt.Println("got a ping, replying")
				r.c.ircWrite(irc.TPong{m.Who, m.Msg})
			case irc.RPong:
				fmt.Println("got a pong")
			case irc.RKick:
				s := sessionEnsure(r.c, m.Where)
				who := irc.Lower(m.Who)
				// xxx accounting for user
				if who == r.c.ic.Lnick {
					sessionStatus(s, fmt.Sprintf("you have been kicked by %s (%s): %s", m.From.Nick, m.From.Text(), m.Msg))
				} else {
					sessionStatus(s, fmt.Sprintf("%s has been kicked by %s (%s): %s", m.Who, m.From.Nick, m.From.Text(), m.Msg))
				}
			case irc.RInvite:
				s := sessionEnsure(r.c, m.Where)
				sessionStatus(s, fmt.Sprintf("%s (%s) invites %s to join %s", m.From.Nick, m.From.Text(), m.Who, m.Where))
			case irc.RUnknown:
				s := r.c.sessions[irc.Lower(m.Where)]
				if s == nil {
					s = r.c.s0
				}
				sessionStatus(s, strings.Join(m.Params, " "))
			case irc.RErrortext:
				cmd, _ := strconv.Atoi(m.Cmd)
				switch cmd {
				case irc.RPLnosuchnick, irc.RPLnosuchchannel, irc.RPLcannotsendtochan, irc.RPLtoomanychannels, irc.RPLwasnosuchnick:
					s := r.c.sessions[irc.Lower(m.Where)]
					if s == nil {
						s = r.c.s0
					}
					sessionStatus(s, strings.Join(m.Params, " "))
				default:
					status(r.c, "error: "+strings.Join(m.Params, " "))
				}
			case irc.RReplytext:
				msg := strings.Join(m.Params, " ")
				cmd, _ := strconv.Atoi(m.Cmd)
				switch cmd {
				case irc.RPLwelcome:
					// xxx status now connected...
					msg = "connected: "+msg
					// xxx do a whois on ourselves to guess max message length
					// xxx start ping process, for latency
				case irc.RPLtopic:
					msg = "topic set: "+msg
				case irc.RPLtopicset:
					msg = "topic set by: "+msg
				case irc.RPLinviting:
					msg = "inviting: "+msg
				case irc.RPLnames, irc.RPLnamesdone:
					// xxx handle names
					msg = "names/namesdone: "+msg
				case irc.RPLaway:
					msg = "away: "+msg
					s := r.c.sessions[irc.Lower(m.Where)]
					if s == nil {
						s = r.c.s0
					}
					sessionStatus(s, "msg")
				case irc.RPLwhoisuser, irc.RPLwhoischannels, irc.RPLwhoisidle, irc.RPLendofwhois, irc.RPLwhoisserver, irc.RPLwhoisoperator:
					msg = "whois: "+msg
				case irc.RPLchannelmode:
					msg = "mode "+msg
				case irc.RPLchannelmodechanged:
					msg = "mode changd "+msg
				}
				s := r.c.sessions[irc.Lower(m.Where)]
				if s == nil {
					s = r.c.s0
				}
				sessionStatus(s, msg)
			default:
				fmt.Println("other irc message?", mm)
			}

		case op := <-postc:
			// new command
			fmt.Println("new command", op)
			s, ok := sessions[op.sid]
			if !ok {
				status0("no such session")
				continue
			}
			if !strings.HasPrefix(op.line, "/") {
				if !s.cantalk {
					sessionStatus(s, "cannot talk here")
					continue
				}
				s.c.ircWrite(irc.TPrivmsg{s.name, op.line})
				eventAdd(EventMsg{"msg", eidNext(), s.sid, op.u, now(), s.c.ic.Nick, op.line})
				fmt.Println("privmsg...")
				continue
			}

			args := strings.Split(op.line[1:], " ")
			if len(args) == 0 {
				sessionStatus(s, "missing command")
				continue
			}
			cmd := args[0]
			params := args[1:]
			params1 := strings.SplitN(op.line[1:], " ", 2)[1:]
			params2 := strings.SplitN(op.line[1:], " ", 3)[1:]
			params3 := strings.SplitN(op.line[1:], " ", 4)[1:]

			testParams := func(n int, needcon bool, usage string) bool {
				r := n==0 && len(params)==0 || n==1 && len(params1)==1 || n==2 && len(params2)==2 || n==3 && len(params3)==3
				if !r {
					sessionStatus(s, "usage: "+usage)
				} else if needcon && s.c == nil {
					sessionStatus(s, "not connected")
					r = false
				}
				return r
			}
			switch cmd {
			case "connect":
				addr := ""
				nick := makeNick()
				switch len(params3) {
				case 0:
					sessionStatus(s, "usage: /connect addr [nick]")
					continue
				case 1:
					addr = params3[0]
				case 2:
					addr = params3[0]
					nick = params3[1]
				}
				if !strings.Contains(addr, ":") {
					addr += ":6667"
				}

				toks := strings.Split(addr, ":")
				toks2 := strings.Split(toks[0], ".")
				i := len(toks2)-1
				if i < 0 {
					i = 0
				}
				cname := toks2[i]
				_, err := strconv.Atoi(cname)
				if err != nil {
					cname = toks[0]
				}
				ns := &Session{sidNext(), cname, false, true, false, nil}
				c := &Connection{sessions: make(map[string]*Session), s0: ns, status: "connecting", addr: addr, nick: nick}
				ns.c = c
				c.sessions[irc.Lower(ns.name)] = ns

				eventAdd(EventCon{"con", eidNext(), ns.sid, "connecting", nick, 0})
				sessionAdd(ns);
				go newIrc(addr, nick, ns, c)

			case "reconnect":
				if !testParams(0, false, "/reconnect") {
					continue
				}
				if s.c == nil {
					sessionStatus(s, "bad target")
					continue
				}
				c := s.c
				addr, nick := c.addr, c.nick
				if c.status == "connecting" || c.status == "connected" {
					sessionStatus(c.s0, "disconnected in order to reconnect")
					eventAdd(EventCon{"con", eidNext(), c.s0.sid, "disconnected", nick, 0})
					if c.ic != nil {
						c.ic.Close()
						c.ic = nil
					}
					c.status = "disconnected"
					c.dead = true
				}
				nc := &Connection{sessions: c.sessions, s0: c.s0, status: "connecting", addr: addr, nick: nick, dead: false}
				for _, ss := range c.sessions {
					ss.c = nc
				}
				go newIrc(addr, nick, nc.s0, nc)

			case "join":
				if !testParams(1, true, "/join channel") {
					continue
				}
				s.c.ircWrite(irc.TJoin{params1[0], ""})
			case "query":
				if !testParams(1, true, "/query nick") {
					continue
				}
				sessionEnsure(s.c, params1[0])
			case "disconnect":
				if !testParams(0, true, fmt.Sprintf("/%s", cmd)) {
					continue
				}
				s.c.ircWrite(irc.TQuit{"gowebirc!"})
			case "away":
				if s.c == nil {
					sessionStatus(s, "not connected")
					continue
				}
				if len(params) == 0 {
					s.c.ircWrite(irc.TAway{""})
				} else if len(params1) == 1 {
					s.c.ircWrite(irc.TAway{params1[0]})
				} else {
					sessionStatus(s, "usage: /away [message]")
				}
			case "nick":
				if !testParams(1, true, "/nick newnick") {
					continue
				}
				s.c.ircWrite(irc.TNick{params1[0]})
			case "umode":
				if !testParams(1, true, "/umode mode") {
					continue
				}
				s.c.ircWrite(irc.TMode{s.c.ic.Nick, []string{params1[0]}})
			case "whois":
				if !testParams(1, true, "/whois nick") {
					continue
				}
				s.c.ircWrite(irc.TWhois{params1[0]})
			case "n", "names":
				if !testParams(0, true, "/"+cmd) {
					continue
				}
				if s.c.s0 == s || !irc.IsChannel(s.name) {
					sessionStatus(s, fmt.Sprintf("not a channel"))
				} else {
					s.c.ircWrite(irc.TNames{s.name})
				}
			case "me":
				if !testParams(1, true, "/me text") {
					continue
				}
				if s == s.c.s0 {
					status(s.c, "not a valid target")
				} else {
					s.c.ircWrite(irc.TPrivmsg{s.name, fmt.Sprintf("\u0001ACTION %s\u0001", params1[0])})
					sessionStatus(s, fmt.Sprintf("%s %s", s.c.ic.Nick, params1[0]))
					// xxx set Uu in response message
				}
			case "notice":
				if !testParams(1, true, "/notice text") {
					continue
				}
				if s == s.c.s0 {
					sessionStatus(s, "not a valid target")
				} else {
					s.c.ircWrite(irc.TNotice{s.name, params1[0]})
					sessionStatus(s, fmt.Sprintf("%s: %s", s.c.ic.Nick, params1[0]))
				}
			case "msg":
				if !testParams(2, true, "/msg who text") {
					continue
				}
				s.c.ircWrite(irc.TPrivmsg{params2[0], params2[1]})
				ss := sessionEnsure(s.c, params2[0])
				eventAdd(EventMsg{"msg", eidNext(), ss.sid, op.u, now(), s.c.ic.Nick, params2[1]})
			case "invite":
				if !testParams(2, true, "/invite nick channel") {
					continue
				}
				s.c.ircWrite(irc.TInvite{params2[0], params2[1]})
			case "version", "time", "ping":
				if !testParams(0, true, "/"+cmd) {
					continue
				}
				if s == s.c.s0 {
					sessionStatus(s, "bad target")
				} else {
					s.c.ircWrite(irc.TPrivmsg{s.name, fmt.Sprintf("\u0001CTCP %s\u0001", strings.ToUpper(cmd))})
					sessionStatus(s, fmt.Sprintf("sent ctcp %s", cmd))
				}
			case "kick":
				if s.c == nil {
					sessionStatus(s, "not connected")
					continue
				}
				if len(params1) != 1 && len(params2) != 2 {
					sessionStatus(s, "usage: /kick who [text]")
					continue
				}
				if s == s.c.s0 || !irc.IsChannel(s.name) {
					sessionStatus(s, "bad target")
				} else {
					who := params1[0]
					msg := ""
					if len(params2) == 2 {
						who = params2[0]
						msg = params2[1]
					}
					s.c.ircWrite(irc.TKick{s.name, who, msg})
				}
			case "op", "deop", "ban", "unban", "voice", "devoice":
				if s.c == nil {
					sessionStatus(s, "not connected")
					continue
				}
				if len(params1) == 0 {
					sessionStatus(s, fmt.Sprintf("usage: /%s who1 ...", cmd))
					continue
				}
				if s == s.c.s0 || !irc.IsChannel(s.name) {
					sessionStatus(s, "bad target")
					continue
				}
				way := "+"
				if strings.HasPrefix(cmd, "de") || strings.HasPrefix(cmd, "un") {
					way = "-"
					cmd = cmd[2:]
				}
				mode := cmd[:1]
				for i := 0; i < len(params); i += 3 {
					modes := []string{}
					for j := 0; j < 3 && i+j < len(params); j += 1 {
						modes = append(modes, way+mode, params[i+j])
					}
					s.c.ircWrite(irc.TMode{s.name, modes})
				}
			case "mode":
				if s.c == nil {
					sessionStatus(s, "not connected")
					continue
				}
				if len(params) == 0 {
					sessionStatus(s, "usage: /mode mode ...")
					continue
				}
				if s == s.c.s0 || !irc.IsChannel(s.name) {
					sessionStatus(s, "bad target")
				} else {
					s.c.ircWrite(irc.TMode{s.name, params})
				}
			case "part":
				if !testParams(0, true, "/part") {
					continue
				}
				if s == s.c.s0 || !irc.IsChannel(s.name) {
					sessionStatus(s, "bad target")
				} else {
					s.c.ircWrite(irc.TPart{s.name})
					sessionStatus(s, "you left")
				}
			case "close":
				if !testParams(0, false, "/close") {
					continue
				}
				if s.c == nil {
					continue
				}
				if s.is0 && len(s.c.sessions) != 1 {
					sessionStatus(s, "not last target")
					continue
				}
				eventAdd(EventRemove{"remove", eidNext(), s.sid})
				delete(s.c.sessions, s.name)
				delete(sessions, s.sid)
			case "closeall":
				if !testParams(0, false, "/closeall") {
					continue
				}
				if s.c == nil {
					continue
				}
				for _, s := range s.c.sessions {
					if s.is0 && !s.c.dead {
						continue
					}
					delete(s.c.sessions, s.name)
					delete(sessions, s.sid)
					if s.is0 {
						s.c.s0 = nil
					}
				}
			case "topic":
				if !testParams(1, true, "/topic text ...") {
					continue
				}
				if s == s.c.s0 || !irc.IsChannel(s.name) {
					sessionStatus(s, "bad target")
				} else {
					s.c.ircWrite(irc.TTopicset{s.name, params1[0]})
				}

			default:
				sessionStatus(s, fmt.Sprintf("unrecognized command: %v", cmd))
			}
		}
	}
}

func index(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func jquery(w http.ResponseWriter, r *http.Request) {
	filename := "jquery-2.0.2.min.js"
	_, err := os.Open(filename)
	if err != nil {
		w.Header().Set("Location", "http://code.jquery.com/"+filename)
		w.WriteHeader(302)
		return
	}
	http.ServeFile(w, r, filename)
}

func help(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "help.html")
}

func authChecker(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := strings.Split(r.Header.Get("Authorization"), " ")
		if httpauth == "" || len(h) == 2 && strings.ToLower(h[0]) == "basic" && h[1] == httpauth {
			fn(w, r)
		} else {
			w.Header().Set("WWW-Authenticate", "Basic realm=\"gowebirc\"")
			w.WriteHeader(401)
		}
	}
}

func makeNick() string {
	if defaultNick != nil && *defaultNick != "" {
		return *defaultNick
	}
	nick := "gopher"
	if u, err := user.Current(); err == nil {
		nick = "go"+u.Username
	}
	return nick
}

func main() {
	addr := flag.String("http", "localhost:8000", "HTTP service address (e.g., ':8000')")
	defaultNick = flag.String("nick", "", "Nick to use on IRC servers.  By default the nick \"go<username>\" is used")
	httpauth0 := flag.String("httpauth", "", "Username and password to require for HTTP basic authentication, in format username:password")
	ssl := flag.Bool("ssl", false, "Whether to serve HTTPS instead of HTTP.")
	certFile := flag.String("cert", "cert.pem", "Only used if -ssl is set.")
	keyFile := flag.String("key", "key.pem", "Only used if -ssl is set.")
	flag.Parse()
	if len(flag.Args()) != 0 {
		flag.Usage()
		os.Exit(1)
	}

	httpauth = *httpauth0
	if httpauth != "" {
		httpauth = base64.StdEncoding.EncodeToString([]byte(httpauth))
	}

	http.Handle("/", authChecker(index))
	http.Handle("/jquery-2.0.2.min.js", authChecker(jquery))
	http.Handle("/help.html", authChecker(help))
	http.Handle("/post", authChecker(post))
	http.Handle("/get", authChecker(get))

	getc = make(chan Get)
	postc = make(chan Post)
	ircrc = make(chan Ircr)
	ircwc = make(chan Ircw)
	ircsc = make(chan Ircs)
	go srv()

	if *ssl {
		fmt.Printf("https://%s/\n", *addr)
		log.Fatal(http.ListenAndServeTLS(*addr, *certFile, *keyFile, nil))
	} else {
		fmt.Printf("http://%s/\n", *addr)
		log.Fatal(http.ListenAndServe(*addr, nil))
	}
}

gowebirc - irc client in go with web frontend


# what is it?

gowebirc is an irc client.  the go program is both serving http to your
browser, and maintains irc connections to the irc server.  updates are
sent to the browser using server-sent events (sse), a light-weight
long-request method that uses standaard chunked http responses and that
most modern browsers support.

the latest version can be found at http://bitbucket.org/mjl/gowebirc/.


# how to run

GOPATH=$PWD go run src/gowebirc.go

this will start gowebirc, listening on http://localhost:8000/.
change the listening address with the -http command-line option.
ask for http user/password auth with -httpauth user:pass.
the default nick used to connect is "go<your-user-name>", or set it with the -nick option.

by default, jquery is loaded from the internet.  you can have it served
by gowebirc by downloading the referenced jquery file to the directory
where index.html resides.

more help information is available in help.html, also accessible through
the frontend.


# license

this code is public domain.
written by mechiel lukkien, mechiel@ueber.net.


# todo

this is work in progress.  things to do:

- improve handling of irc messages
- improve bookkeeping of events, also when sessions have been closed
- check whether we always have a From when we think.  make From not a pointer anymore in the irc message structs?
- after giving new state, more carefully collect events:  not the ones for now-closed windows.

- bug: when another nick renames it isn't propagated with events

- autoreconnect, autojoin
- log all talk to log file
- don't keep all events in memory forever
- show latency to the webserver and the irc servers.
- add more content helpers, eg for video's.
- store all history in file system or sqlite?  with event ids, so we get infinite history.
- allow customisations with css or js hooks

#!/bin/sh
# One node of the yamn test network, run as its user (yamn-test) from
# ~/testnet, where testnet.sh puts bin/yamn, bin/tnode, nodes.txt and this
# script.
#
#   node.sh setup NAME   write the configs for node NAME (from nodes.txt)
#   node.sh init         create the remailer's keys (one remailer run)
#   node.sh start|stop|restart|status
#   node.sh send N [C]   hub: send N test messages to the inbox, with
#                        client C: client (step 1 keyring, default) or
#                        pclient (the pingers in pingers.txt)
#   node.sh refresh [C]  hub: download C's keyring and stats now
#   node.sh inbox        hub: list what arrived in the inbox
set -u
T=$HOME/testnet
B=$T/bin
cd "$T" || exit 1

die() { echo "$*" >&2; exit 1; }
field() { awk -v n="$1" -v f="$2" '!/^#/ && $1 == n { print $f }' "$T/nodes.txt"; }
has_role() { echo ",$ROLES," | grep -q ",$1,"; }

load() {
	[ -f "$T/node.env" ] || die "run node.sh setup NAME first"
	. "$T/node.env"
}

setup() {
	NAME=$1
	ROLES=$(field "$NAME" 3)
	[ -n "$ROLES" ] || die "node $NAME isn't in nodes.txt"
	HUB=$(awk '!/^#/ && $3 ~ /(^|,)hub(,|$)/ { print $2; exit }' "$T/nodes.txt")
	[ -n "$HUB" ] || die "no hub in nodes.txt"
	printf 'NAME=%s\nROLES=%s\nHUB=%s\n' "$NAME" "$ROLES" "$HUB" >"$T/node.env"
	EXIT=false
	has_role exit && EXIT=true
	mkdir -p "$T/remailer" "$T/mx" "$T/run" "$T/log"
	cat >"$T/remailer/yamn.yml" <<-EOF
	# yamn test network: remailer $NAME (written by node.sh setup)
	general:
	    loglevel: info
	    logtofile: true
	remailer:
	    name: "$NAME"
	    address: mix@$NAME.yamn.test
	    exit: $EXIT
	    daemon: false
	mail:
	    smtp_relay: 127.0.0.1
	    smtp_port: "2525"
	    mx_relay: false
	    outbound_name: Test Remailer $NAME
	    outbound_addy: noreply@$NAME.yamn.test
	urls:
	    fetch: true
	    pubring: http://$HUB:8080/pubring.mix
	    mlist2: http://$HUB:8080/mlist2.txt
	pool:
	    # Send whatever is there every 2 minutes: a test network has too
	    # little traffic for the default pool of 5 plus 5.
	    size: 0
	    min_send: 1
	    rate: 90
	    loop: 120
	EOF
	if has_role hub; then
		mkdir -p "$T/www" "$T/inbox/Maildir" "$T/client"
		cat >"$T/client/yamn.yml" <<-EOF
		# yamn test network: client on $NAME (written by node.sh setup)
		general:
		    loglevel: info
		mail:
		    smtp_relay: 127.0.0.1
		    smtp_port: "2525"
		    mx_relay: false
		urls:
		    fetch: true
		    pubring: http://$HUB:8080/pubring.mix
		    mlist2: http://$HUB:8080/mlist2.txt
		EOF
		if [ -s "$T/pingers.txt" ]; then
			mkdir -p "$T/pclient"
			{
				cat <<-EOF
				# yamn test network: client on $NAME using the pingers in
				# pingers.txt (written by node.sh setup)
				general:
				    loglevel: info
				mail:
				    smtp_relay: 127.0.0.1
				    smtp_port: "2525"
				    mx_relay: false
				urls:
				    fetch: true
				    key_quorum: 2
				    require_signatures: true
				    pingers:
				EOF
				awk '!/^#/ && NF >= 2 { print $1, $2 }' "$T/pingers.txt" | while read -r p key; do
					ip=$(field "$p" 2)
					[ -n "$ip" ] || die "pinger $p isn't in nodes.txt"
					printf '        - name: "%s"\n' "$p"
					printf '          pubring: http://%s:8080/pinger/pubring.mix\n' "$ip"
					printf '          mlist2: http://%s:8080/pinger/mlist2.txt\n' "$ip"
					printf '          keys: ["%s"]\n' "$key"
				done
			} >"$T/pclient/yamn.yml"
		fi
	fi
	echo "$NAME: set up ($ROLES, exit=$EXIT, hub $HUB)"
}

yamn_remailer() { "$B/yamn" --config "$T/remailer/yamn.yml" --dir "$T/remailer" "$@"; }

# Pinger nodes: go-echolot's Maildir and results, once step 2 created them.
PINGER_HOME=/var/lib/go-echolot

# supervise NAME CMD...: run CMD in its own session, restarting it after a
# crash; its pid file is run/NAME.pid, its output log/NAME.log.
supervise() {
	name=$1
	shift
	if running "$name"; then
		echo "$name already running"
		return
	fi
	setsid sh -c '
		while :; do
			"$@"
			echo "$(date -u "+%F %T") exited with $?, restarting in 10 s"
			sleep 10
		done' sh "$@" </dev/null >>"$T/log/$name.log" 2>&1 &
	echo $! >"$T/run/$name.pid"
	echo "$name started"
}

running() {
	[ -f "$T/run/$1.pid" ] && kill -0 "$(cat "$T/run/$1.pid")" 2>/dev/null
}

halt() {
	if running "$1"; then
		kill -TERM -- "-$(cat "$T/run/$1.pid")" 2>/dev/null || kill -TERM "$(cat "$T/run/$1.pid")"
		echo "$1 stopped"
	fi
	rm -f "$T/run/$1.pid"
}

start() {
	load
	set -- -nodes "$T/nodes.txt" -self "$NAME" -spool "$T/mx/spool" -deliver "mix=$T/remailer/Maildir"
	has_role hub && set -- "$@" -deliver "inbox=$T/inbox/Maildir"
	if has_role pinger && [ -d "$PINGER_HOME/Maildir" ]; then
		set -- "$@" -deliver "pinger=$PINGER_HOME/Maildir"
	fi
	supervise mx "$B/tnode" mx "$@"
	set --
	has_role hub && set -- -serve "/=$T/www" -mlist2 "$T/www"
	has_role pinger && [ -d "$PINGER_HOME/results" ] && set -- "$@" -serve "/pinger/=$PINGER_HOME/results"
	[ $# -gt 0 ] && supervise www "$B/tnode" www "$@"
	supervise yamn "$B/yamn" --config "$T/remailer/yamn.yml" --dir "$T/remailer" -M -D
}

stop() {
	for d in yamn www mx; do halt $d; done
}

status() {
	load
	s=""
	for d in mx www yamn; do
		if running $d; then s="$s $d:up"; elif [ -f "$T/log/$d.log" ]; then s="$s $d:DOWN"; fi
	done
	count() { ls "$1" 2>/dev/null | wc -l | tr -d ' '; }
	echo "$NAME:$s | pool $(count "$T/remailer/pool") | maildir new $(count "$T/remailer/Maildir/new") | spool $(count "$T/mx/spool") | key $(head -c 40 "$T/remailer/key.txt" 2>/dev/null | cut -d' ' -f1-3)"
}

client_dir() {
	c=${1:-client}
	case $c in client | pclient) ;; *) die "no client $c" ;; esac
	[ -f "$T/$c/yamn.yml" ] || die "$c isn't set up here"
	echo "$T/$c"
}

send() {
	load
	has_role hub || die "send runs on the hub"
	n=${1:-1}
	C=$(client_dir "${2:-}") || exit 1
	i=1
	while [ "$i" -le "$n" ]; do
		printf 'Subject: testnet %s (%s)\n\nTest message %s of %s from the hub via %s, %s.\n' \
			"$i" "${C##*/}" "$i" "$n" "${C##*/}" "$(date -u '+%F %T UTC')" |
			"$B/yamn" --config "$C/yamn.yml" --dir "$C" -m -S -t "inbox@$NAME.yamn.test"
		i=$((i + 1))
	done
}

inbox() {
	load
	for f in "$T"/inbox/Maildir/new/*; do
		[ -f "$f" ] || continue
		printf '%s  %s  %s\n' "$(basename "$f" | cut -c1-12)" \
			"$(grep -m1 '^Subject:' "$f")" "$(grep -m1 '^From:' "$f")"
	done
	echo "$(ls "$T/inbox/Maildir/new" | wc -l | tr -d ' ') message(s)"
}

case ${1:-} in
setup) setup "${2:?node name}" ;;
init) load; yamn_remailer -M >>"$T/log/init.log" 2>&1; ls -l "$T/remailer/key.txt" ;;
start) start ;;
stop) stop ;;
restart) stop; sleep 1; start ;;
status) status ;;
send) send "${2:-1}" "${3:-}" ;;
refresh)
	load
	C=$(client_dir "${2:-}") || exit 1
	"$B/yamn" --config "$C/yamn.yml" --dir "$C" --refresh 2>&1 | grep -v 'plain http'
	ls -l "$C"
	;;
inbox) inbox ;;
*) die "usage: node.sh setup NAME | init | start | stop | restart | status | send N [client|pclient] | refresh [client|pclient] | inbox" ;;
esac

#!/bin/sh
# Drive the yamn test network from a workstation that reaches the nodes as
# root over ssh.  Everything on a node runs as $TUSER in ~$TUSER/testnet.
#
#   testnet.sh build                     static yamn and tnode into testnet/build
#   testnet.sh deploy [NODE...]          copy binaries and scripts, write configs
#   testnet.sh init [NODE...]            create the remailers' keys
#   testnet.sh keyring                   collect the keys into the hub's pubring.mix
#   testnet.sh boot|noboot [NODE...]     start the node at boot, or not (deploy does boot)
#   testnet.sh start|stop|restart|status [NODE...]
#   testnet.sh send [N] [client|pclient] N test messages from the hub to its inbox
#   testnet.sh refresh [client|pclient]  the hub client's keyring download, now
#   testnet.sh inbox                     what arrived
#   testnet.sh log NODE [mx|www|yamn|remailer]
#   testnet.sh clean [NODE...]           stop and remove ~$TUSER/testnet
#
# Setting up from scratch: build, deploy, init, keyring, start, then send.
# Nodes start by themselves after a reboot, in any order: a remailer that
# can't reach the hub yet keeps its keyring and tries again.
set -eu
# The script calls itself for each node, after the cd below.
SELF=$(cd "$(dirname "$0")" && pwd)/$(basename "$0")
cd "$(dirname "$SELF")"
TUSER=${TUSER:-yamn-test}
PAR=${PAR:-6}
SSH="ssh -o BatchMode=yes -o ConnectTimeout=10"

nodes() { awk '!/^#/ && NF >= 3 { print $1 }' nodes.txt; }
ip() { awk -v n="$1" '!/^#/ && $1 == n { print $2 }' nodes.txt; }
hub() { awk '!/^#/ && $3 ~ /(^|,)hub(,|$)/ { print $1; exit }' nodes.txt; }

# on NODE CMD: run CMD as $TUSER in ~/testnet on NODE.
on() { $SSH "root@$(ip "$1")" "su - $TUSER -c 'cd testnet && $2'"; }

# bootscript on|off: the script, run as root on a node, that makes
# "node.sh start" run at boot (systemd unit, or OpenRC's local.d), or
# removes that again.  node.sh stop runs at shutdown.
bootscript() {
	printf 'set -eu\nu=%s\nmode=%s\n' "$TUSER" "$1"
	cat <<-'EOF'
	home=$(awk -F: -v u="$u" '$1 == u { print $6 }' /etc/passwd)
	[ -n "$home" ] || { echo "no user $u"; exit 1; }
	unit=/etc/systemd/system/yamn-testnet.service
	ld=/etc/local.d/yamn-testnet
	if [ -d /run/systemd/system ]; then
		if [ "$mode" = on ]; then
			cat >"$unit" <<-UNIT
			# yamn test network (written by testnet.sh boot)
			[Unit]
			Description=yamn test network node
			Wants=network-online.target
			After=network-online.target

			[Service]
			Type=oneshot
			RemainAfterExit=yes
			User=$u
			ExecStart=/bin/sh $home/testnet/node.sh start
			ExecStop=/bin/sh $home/testnet/node.sh stop

			[Install]
			WantedBy=multi-user.target
			UNIT
			systemctl daemon-reload
			systemctl enable -q yamn-testnet.service
			echo "boot: systemd yamn-testnet.service enabled"
		else
			systemctl disable -q yamn-testnet.service 2>/dev/null || true
			rm -f "$unit"
			systemctl daemon-reload
			echo "boot: systemd unit removed"
		fi
	elif command -v rc-update >/dev/null; then
		if [ "$mode" = on ]; then
			for a in start stop; do
				printf '#!/bin/sh\n# yamn test network (written by testnet.sh boot)\nsu - %s -c "cd testnet && sh node.sh %s" >/dev/null 2>&1\n' "$u" "$a" >"$ld.$a"
				chmod 755 "$ld.$a"
			done
			rc-update add local default >/dev/null 2>&1 || true
			echo "boot: OpenRC $ld.start"
		else
			rm -f "$ld.start" "$ld.stop"
			echo "boot: OpenRC local.d scripts removed"
		fi
	else
		echo "boot: no systemd or OpenRC; start this node by hand after a reboot"
	fi
	EOF
}

# each CMD NODE...: run "testnet.sh CMD NODE" for each node, PAR at a time.
each() {
	cmd=$1
	shift
	[ $# -gt 0 ] || set -- $(nodes)
	printf '%s\n' "$@" | xargs -P "$PAR" -I{} sh "$SELF" "_$cmd" {}
}

case ${1:-} in
build)
	mkdir -p build
	(cd .. && CGO_ENABLED=0 go build -trimpath -o testnet/build/yamn .)
	CGO_ENABLED=0 go build -trimpath -o build/tnode ./tnode
	./build/yamn --version
	;;
start | restart)
	# The hub first: the others download the keyring from it as they start.
	c=$1
	shift
	if [ $# -eq 0 ]; then
		h=$(hub)
		sh "$SELF" "_$c" "$h"
		set -- $(nodes | grep -vx "$h")
	fi
	each "$c" "$@"
	;;
deploy | init | boot | noboot | stop | status | clean)
	c=$1
	shift
	each "$c" "$@"
	;;
_deploy)
	n=$2
	h=root@$(ip "$n")
	$SSH "$h" "id $TUSER >/dev/null 2>&1 || (useradd -m -s /bin/sh $TUSER 2>/dev/null || adduser -D -s /bin/sh $TUSER); mkdir -p ~$TUSER/testnet/bin"
	# Upload under new names and rename: a running binary can't be
	# overwritten ("text file busy"), but it can be replaced.
	scp -q -o BatchMode=yes build/yamn "$h:~$TUSER/testnet/bin/yamn.new"
	scp -q -o BatchMode=yes build/tnode "$h:~$TUSER/testnet/bin/tnode.new"
	scp -q -o BatchMode=yes nodes.txt pingers.txt node.sh "$h:~$TUSER/testnet/"
	$SSH "$h" "cd ~$TUSER/testnet/bin && chmod 755 yamn.new tnode.new && mv yamn.new yamn && mv tnode.new tnode && chown -R $TUSER: ~$TUSER/testnet"
	on "$n" "sh node.sh setup $n"
	sh "$SELF" _boot "$n"
	;;
_boot) bootscript on | $SSH "root@$(ip "$2")" sh -s | sed "s/^/$2: /" ;;
_noboot) bootscript off | $SSH "root@$(ip "$2")" sh -s | sed "s/^/$2: /" ;;
_init) on "$2" "sh node.sh init" ;;
_start) on "$2" "sh node.sh start" | sed "s/^/$2: /" ;;
_stop) on "$2" "sh node.sh stop" | sed "s/^/$2: /" ;;
_restart) on "$2" "sh node.sh restart" | sed "s/^/$2: /" ;;
_status) on "$2" "sh node.sh status" ;;
_clean)
	sh "$SELF" _noboot "$2"
	on "$2" "sh node.sh stop; cd && rm -rf testnet" && echo "$2: removed"
	;;
keyring)
	tmp=$(mktemp)
	for n in $(nodes); do
		on "$n" "cat remailer/key.txt" >>"$tmp" && echo >>"$tmp"
	done
	h=$(hub)
	echo "$(grep -c '^-----Begin Mix Key-----' "$tmp") keys -> $h:www/pubring.mix"
	scp -q -o BatchMode=yes "$tmp" "root@$(ip "$h"):/tmp/testnet-pubring.mix"
	rm -f "$tmp"
	$SSH "root@$(ip "$h")" "install -o $TUSER -m 0644 /tmp/testnet-pubring.mix ~$TUSER/testnet/www/pubring.mix && rm /tmp/testnet-pubring.mix"
	on "$h" "bin/tnode mlist2 www && head -3 www/mlist2.txt"
	;;
send) on "$(hub)" "sh node.sh send ${2:-1} ${3:-}" ;;
refresh) on "$(hub)" "sh node.sh refresh ${2:-}" ;;
inbox) on "$(hub)" "sh node.sh inbox" ;;
log)
	n=${2:?node}
	case ${3:-yamn} in
	remailer) on "$n" "tail -50 remailer/yamn.log" ;;
	*) on "$n" "tail -50 log/${3:-yamn}.log" ;;
	esac
	;;
*) sed -n '2,19s/^# \{0,1\}//p' "$SELF"; exit 2 ;;
esac

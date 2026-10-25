#!/bin/sh
# Drive the yamn test network from a workstation that reaches the nodes as
# root over ssh.  Everything on a node runs as $TUSER in ~$TUSER/testnet.
#
#   testnet.sh build                     static yamn and tnode into testnet/build
#   testnet.sh deploy [NODE...]          copy binaries and scripts, write configs
#   testnet.sh init [NODE...]            create the remailers' keys
#   testnet.sh keyring                   collect the keys into the hub's pubring.mix
#   testnet.sh start|stop|restart|status [NODE...]
#   testnet.sh send [N]                  N test messages from the hub to its inbox
#   testnet.sh inbox                     what arrived
#   testnet.sh log NODE [mx|www|yamn|remailer]
#   testnet.sh clean [NODE...]           stop and remove ~$TUSER/testnet
#
# Setting up from scratch: build, deploy, init, keyring, start, then send.
set -eu
cd "$(dirname "$0")"
TUSER=${TUSER:-yamn-test}
PAR=${PAR:-6}
SSH="ssh -o BatchMode=yes -o ConnectTimeout=10"

nodes() { awk '!/^#/ && NF >= 3 { print $1 }' nodes.txt; }
ip() { awk -v n="$1" '!/^#/ && $1 == n { print $2 }' nodes.txt; }
hub() { awk '!/^#/ && $3 ~ /(^|,)hub(,|$)/ { print $1; exit }' nodes.txt; }

# on NODE CMD: run CMD as $TUSER in ~/testnet on NODE.
on() { $SSH "root@$(ip "$1")" "su - $TUSER -c 'cd testnet && $2'"; }

# each CMD NODE...: run "testnet.sh CMD NODE" for each node, PAR at a time.
each() {
	cmd=$1
	shift
	[ $# -gt 0 ] || set -- $(nodes)
	printf '%s\n' "$@" | xargs -P "$PAR" -I{} sh "$0" "_$cmd" {}
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
		sh "$0" "_$c" "$h"
		set -- $(nodes | grep -vx "$h")
	fi
	each "$c" "$@"
	;;
deploy | init | stop | status | clean)
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
	scp -q -o BatchMode=yes nodes.txt node.sh "$h:~$TUSER/testnet/"
	$SSH "$h" "cd ~$TUSER/testnet/bin && chmod 755 yamn.new tnode.new && mv yamn.new yamn && mv tnode.new tnode && chown -R $TUSER: ~$TUSER/testnet"
	on "$n" "sh node.sh setup $n"
	;;
_init) on "$2" "sh node.sh init" ;;
_start) on "$2" "sh node.sh start" | sed "s/^/$2: /" ;;
_stop) on "$2" "sh node.sh stop" | sed "s/^/$2: /" ;;
_restart) on "$2" "sh node.sh restart" | sed "s/^/$2: /" ;;
_status) on "$2" "sh node.sh status" ;;
_clean) on "$2" "sh node.sh stop; cd && rm -rf testnet" && echo "$2: removed" ;;
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
send) on "$(hub)" "sh node.sh send ${2:-1}" ;;
inbox) on "$(hub)" "sh node.sh inbox" ;;
log)
	n=${2:?node}
	case ${3:-yamn} in
	remailer) on "$n" "tail -50 remailer/yamn.log" ;;
	*) on "$n" "tail -50 log/${3:-yamn}.log" ;;
	esac
	;;
*) sed -n '2,17s/^# \{0,1\}//p' "$0"; exit 2 ;;
esac

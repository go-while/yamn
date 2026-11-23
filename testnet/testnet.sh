#!/bin/sh
# Drive the yamn test network from a workstation that reaches the nodes as
# root over ssh.  Everything on a node runs as $TUSER in ~$TUSER/testnet.
#
#   testnet.sh build                     static yamn and tnode into testnet/build
#   testnet.sh deploy [NODE...]          copy binaries and scripts, write configs
#   testnet.sh init [NODE...]            create the remailers' keys
#   testnet.sh keyring                   collect the keys into the hub's pubring.mix
#   testnet.sh start|stop|restart|status [NODE...]
#   testnet.sh send [N] [client|pclient] N test messages from the hub to its inbox
#   testnet.sh refresh [client|pclient]  the hub client's keyring download, now
#   testnet.sh inbox                     what arrived
#   testnet.sh log NODE [mx|www|yamn|remailer]
#   testnet.sh clean [NODE...]           stop and remove ~$TUSER/testnet
#
# Setting up from scratch: build, deploy, init, keyring, start, then send.
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

# each CMD NODE...: run "testnet.sh CMD NODE" for each node, PAR at a time.
each() {
	cmd=$1
	shift
	[ $# -gt 0 ] || set -- $(nodes)
	printf '%s\n' "$@" | xargs -P "$PAR" -I{} sh "$SELF" "_$cmd" {}
}

# Build hosts: the KVM VMs behind $JUMP first, a free nuc container (any)
# when the one we got does not answer.
JUMP=${JUMP:-root@mbox-mo1.pugleaf.net}
BUILD_HOSTS=${BUILD_HOSTS:-10.10.30.109,10.10.30.119,10.10.30.102,10.10.30.121}
GOBIN_PATH='PATH=$PATH:/usr/local/go/bin:/root/yamn-test/go/bin'

# take HOSTS [JUMPHOST]: lock one of HOSTS through jobq.sh and check that
# it answers. Sets bh, bid and BJ (ssh/scp jump option); on failure
# nothing stays locked.
take() {
	BJ=${2:+-J $2}
	r=$(jobq.sh lock yamn "${JOBQ_AGENT:-yamn-1}" "$1") || return 1
	set -- $r
	[ $# -eq 2 ] || return 1
	bh=$1
	bid=$2
	$SSH $BJ "root@$bh" true && return 0
	echo "build: $bh does not answer" >&2
	jobq.sh unlock "$bh" "$bid"
	return 1
}

# goinstall VERSION: put the official Go tarball (sha256 from go.dev) into
# /usr/local/go on $bh. Only where there is no /usr/local/go yet.
goinstall() {
	f=go$1.linux-amd64.tar.gz
	sum=$(curl -fsS 'https://go.dev/dl/?mode=json&include=all' |
		python3 -Ic 'import json, sys
print(next(x["sha256"] for r in json.load(sys.stdin) for x in r["files"] if x["filename"] == sys.argv[1]))' "$f") ||
		{ echo "build: no sha256 for $f on go.dev" >&2; return 1; }
	echo "build: installing $f on $bh"
	$SSH $BJ "root@$bh" "[ \"\$(uname -m)\" = x86_64 ] || { echo 'build: not x86_64' >&2; exit 1; }
		[ ! -e /usr/local/go ] || { echo 'build: /usr/local/go exists but has no go' >&2; exit 1; }
		cd /tmp && rm -f $f &&
		{ curl -fsSLO https://go.dev/dl/$f || wget -q https://go.dev/dl/$f; } &&
		echo '$sum  $f' | sha256sum -c - >/dev/null || exit 1
		# Unpack beside it and rename, so a failed tar leaves no half /usr/local/go.
		n=/usr/local/.go-new.\$\$
		{ mkdir \$n && tar -C \$n -xzf $f && mv \$n/go /usr/local/go; } || { rm -rf \$n $f; exit 1; }
		rmdir \$n; rm -f $f; /usr/local/go/bin/go version"
}

case ${1:-} in
build)
	# Build on a test host from the job queue (jobq.sh), never here: ship
	# the committed HEAD there, build both static binaries, copy them back.
	# Uncommitted changes are not included. A host without Go gets the
	# official tarball of go.mod's version.
	mkdir -p build
	command -v jobq.sh >/dev/null || { echo "build: jobq.sh not found" >&2; exit 1; }
	rev=$(git rev-parse --short HEAD)
	gov=$(git show HEAD:go.mod | awk '$1 == "go" { print $2; exit }')
	git diff --quiet HEAD -- .. || echo "build: uncommitted changes are not built (HEAD $rev)" >&2
	take "$BUILD_HOSTS" "$JUMP" || take any || { echo "build: no host from jobq.sh" >&2; exit 1; }
	d=/root/yamn-build
	# The unlock must run even if the cleanup fails (set -e holds in traps),
	# and on Ctrl-C or kill too.
	trap '$SSH $BJ "root@$bh" "rm -rf $d" || :; jobq.sh unlock "$bh" "$bid"' EXIT
	trap 'exit 130' INT TERM HUP
	echo "build: $rev on $bh"
	$SSH $BJ "root@$bh" "$GOBIN_PATH; command -v go >/dev/null" || goinstall "$gov"
	git -C .. archive --format=tar HEAD | $SSH $BJ "root@$bh" "rm -rf $d && mkdir -p $d && tar -x -C $d"
	$SSH $BJ "root@$bh" "cd $d && $GOBIN_PATH &&
		export CGO_ENABLED=0 &&
		timeout 30m go build -trimpath -o out/yamn . &&
		timeout 30m go build -trimpath -o out/tnode ./testnet/tnode &&
		out/yamn --version --i-am-root"
	scp -q -o BatchMode=yes $BJ "root@$bh:$d/out/yamn" "root@$bh:$d/out/tnode" build/
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
	scp -q -o BatchMode=yes nodes.txt pingers.txt node.sh "$h:~$TUSER/testnet/"
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
*) sed -n '2,18s/^# \{0,1\}//p' "$SELF"; exit 2 ;;
esac

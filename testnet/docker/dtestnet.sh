#!/bin/sh
# Drive the docker variant of the yamn test network: one container per
# public server (root@web-<name>, podman or docker), host networking, the
# ports of ports.env, data in /srv/yamn-testnet/<name> on the server.
#
#   dtestnet.sh busybox            static busybox from an Alpine test VM (jobq.sh)
#   dtestnet.sh image              build/yamn-tn.tar from ../build/{yamn,tnode}
#                                  (../testnet.sh build) and build/busybox
#   dtestnet.sh deploy [NODE...]   load the image on the servers, (re)start the containers
#   dtestnet.sh keyring            collect the keys into the hub's pubring.mix
#   dtestnet.sh start|stop|status [NODE...]
#   dtestnet.sh send [N]           N test messages from the hub to its inbox
#   dtestnet.sh inbox              what arrived
#   dtestnet.sh log NODE [mx|www|yamn|remailer]
#   dtestnet.sh clean [NODE...]    remove the containers, images and data
#
# Setting up: ../testnet.sh build, busybox, image, deploy, keyring, send.
set -eu
SELF=$(cd "$(dirname "$0")" && pwd)/$(basename "$0")
cd "$(dirname "$SELF")"
SSH="ssh -o BatchMode=yes -o ConnectTimeout=10"
HOSTS=${HOSTS:-web-%s}   # server of node NAME: printf "$HOSTS" NAME
D=/srv/yamn-testnet      # on the servers
CUID=10001               # yamn-test in the container
APPLETS="sh ash awk grep egrep pgrep pkill setsid kill sleep date ls head
tail cut wc tr cat mkdir rm mv cp chmod id printf echo basename dirname env
sed ps ln touch true false test [ expr sort uniq find xargs tee timeout du
df readlink stat md5sum hostname"

die() { echo "$*" >&2; exit 1; }
nodes() { awk '!/^#/ && NF >= 3 { print $1 }' nodes.txt; }
hub() { awk '!/^#/ && $3 ~ /(^|,)hub(,|$)/ { print $1; exit }' nodes.txt; }
host() { echo "root@$(printf "$HOSTS" "$1")"; }
ctr() { echo "yamn-tn-$1"; }
# on NODE CMD: run CMD as yamn-test in ~/testnet in NODE's container.
on() { $SSH "$(host "$1")" "podman exec -u $CUID $(ctr "$1") sh -c 'cd ~/testnet && $2'"; }
each() {
	cmd=$1
	shift
	[ $# -gt 0 ] || set -- $(nodes)
	for n; do sh "$SELF" "_$cmd" "$n" || echo "$n: $cmd failed" >&2; done
}

case ${1:-} in
busybox)
	mkdir -p build
	J="-J ${JUMP:-root@mbox-mo1.pugleaf.net}"
	set -- $(jobq.sh lock yamn "${JOBQ_AGENT:-yamn-1}" "${BB_HOSTS:-10.10.30.105,10.10.30.104,10.10.30.103}")
	[ $# -eq 2 ] || die "busybox: no host from jobq.sh"
	bh=$1
	bid=$2
	trap 'jobq.sh unlock "$bh" "$bid"' EXIT
	trap 'exit 130' INT TERM HUP
	$SSH $J "root@$bh" "apk add -q busybox-static && /bin/busybox.static | head -1"
	scp -q -o BatchMode=yes $J "root@$bh:/bin/busybox.static" build/busybox
	chmod 755 build/busybox
	;;
image)
	for f in ../build/yamn ../build/tnode build/busybox; do
		[ -x "$f" ] || die "image: $f missing (../testnet.sh build, dtestnet.sh busybox)"
	done
	r=build/rootfs
	rm -rf "$r"
	mkdir -p "$r/bin" "$r/etc" "$r/tmp" "$r/home/yamn-test/testnet" "$r/opt/yamn-tn/bin"
	chmod 1777 "$r/tmp"
	cp build/busybox "$r/bin/busybox"
	for a in $APPLETS; do ln -s busybox "$r/bin/$a"; done
	printf 'root:x:0:0:root:/:/bin/sh\nyamn-test:x:%s:%s::/home/yamn-test:/bin/sh\n' $CUID $CUID >"$r/etc/passwd"
	printf 'root:x:0:\nyamn-test:x:%s:\n' $CUID >"$r/etc/group"
	cp ../build/yamn ../build/tnode "$r/opt/yamn-tn/bin/"
	cp ../node.sh nodes.txt ports.env entrypoint.sh "$r/opt/yamn-tn/"
	tag=$(date -u +%Y%m%d%H%M)
	echo "$tag $(git describe --always --dirty), yamn $(stat -c %y ../build/yamn | cut -c1-16)" >"$r/opt/yamn-tn/VERSION"
	chmod -R go-w "$r/opt" "$r/etc"
	tar --numeric-owner --owner=0 --group=0 -C "$r" -cf build/yamn-tn.tar .
	echo "$tag" >build/yamn-tn.tag
	rm -rf "$r"
	echo "build/yamn-tn.tar: yamn-tn:$tag, $(du -h build/yamn-tn.tar | cut -f1)"
	;;
deploy) shift; each deploy "$@" ;;
_deploy)
	n=$2
	h=$(host "$n")
	c=$(ctr "$n")
	tag=$(cat build/yamn-tn.tag)
	$SSH "$h" "mkdir -p $D/$n && chown $CUID:$CUID $D/$n && chmod 700 $D/$n"
	scp -q -o BatchMode=yes build/yamn-tn.tar "$h:$D/yamn-tn.tar"
	# Keep ssh's exit status (a pipe into sed would hide it): each reports failures.
	out=$($SSH "$h" "set -e
		podman import -q --change 'USER $CUID:$CUID' --change 'ENV HOME=/home/yamn-test' \
			--change 'WORKDIR /home/yamn-test' --change 'ENTRYPOINT [\"/opt/yamn-tn/entrypoint.sh\"]' \
			$D/yamn-tn.tar localhost/yamn-tn:$tag >/dev/null
		rm -f $D/yamn-tn.tar
		podman rm -f -t 20 $c >/dev/null 2>&1 || true
		podman run -d --name $c --network host -e NODE=$n -v $D/$n:/home/yamn-test/testnet \
			--memory 256m --cpus 0.5 --pids-limit 128 \
			--log-driver k8s-file --log-opt max-size=10mb \
			--restart unless-stopped localhost/yamn-tn:$tag >/dev/null
		for i in \$(podman images --format '{{.Repository}}:{{.Tag}}' | grep '^localhost/yamn-tn:' | grep -v ':$tag\$'); do
			podman rmi \$i >/dev/null 2>&1 || true
		done
		sleep 3
		podman logs --tail 5 $c" 2>&1) || rc=$?
	printf '%s\n' "$out" | sed "s/^/$n: /"
	exit "${rc:-0}"
	;;
keyring)
	tmp=$(mktemp)
	for n in $(nodes); do
		$SSH "$(host "$n")" "cat $D/$n/remailer/key.txt" >>"$tmp" && echo >>"$tmp"
	done
	h=$(hub)
	echo "$(grep -c '^-----Begin Mix Key-----' "$tmp") keys -> $h:www/pubring.mix"
	scp -q -o BatchMode=yes "$tmp" "$(host "$h"):$D/pubring.mix.new"
	rm -f "$tmp"
	$SSH "$(host "$h")" "install -o $CUID -g $CUID -m 0644 $D/pubring.mix.new $D/$h/www/pubring.mix && rm $D/pubring.mix.new"
	on "$h" "bin/tnode mlist2 www && head -3 www/mlist2.txt"
	;;
start | stop | status) c=$1; shift; each "$c" "$@" ;;
_start) $SSH "$(host "$2")" "podman start $(ctr "$2") >/dev/null" && echo "$2: started" ;;
_stop) $SSH "$(host "$2")" "podman stop -t 20 $(ctr "$2") >/dev/null" && echo "$2: stopped" ;;
_status)
	s=$($SSH "$(host "$2")" "podman ps -a --filter name=^$(ctr "$2")\$ --format '{{.Status}}'")
	case $s in
	Up*)
		out=$(on "$2" "sh node.sh status") || rc=$?
		printf '%s [%s]\n' "$out" "$s"
		exit "${rc:-0}"
		;;
	*) echo "$2: container ${s:-missing}"; exit 1 ;;
	esac
	;;
send) on "$(hub)" "sh node.sh send ${2:-1}" ;;
inbox) on "$(hub)" "sh node.sh inbox" ;;
log)
	n=${2:?node}
	case ${3:-yamn} in
	mx | www | yamn) on "$n" "tail -n 30 log/${3:-yamn}.log" ;;
	remailer) on "$n" "tail -n 30 remailer/yamn.log" ;;
	*) die "log: mx, www, yamn or remailer" ;;
	esac
	;;
clean) shift; each clean "$@" ;;
_clean)
	n=$2
	$SSH "$(host "$n")" "podman rm -f -t 20 $(ctr "$n") >/dev/null 2>&1 || true
		for i in \$(podman images --format '{{.Repository}}:{{.Tag}}' | grep '^localhost/yamn-tn:'); do podman rmi \$i >/dev/null; done
		rm -rf $D/$n; rmdir $D 2>/dev/null || true"
	echo "$n: removed"
	;;
*) sed -n '2,17p' "$SELF" | sed 's/^# \{0,1\}//'; exit 1 ;;
esac

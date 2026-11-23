#!/bin/sh
# Entrypoint of a docker testnet node; runs as yamn-test with NODE set and
# the node's data directory mounted on ~/testnet.
set -u
: "${NODE:?NODE is not set}"
T=$HOME/testnet
O=/opt/yamn-tn
cd "$T" || exit 1
ln -sfn "$O/bin" bin
cp "$O/node.sh" "$O/nodes.txt" "$O/ports.env" .
# A new container has none of the old processes, but its pids are small
# and may match the old pid files: node.sh would think they still run.
rm -f run/*.pid
sh node.sh setup "$NODE" || exit 1
[ -s remailer/key.txt ] || sh node.sh init
sh node.sh start
echo "image $(cat "$O/VERSION")"
trap 'sh node.sh stop; exit 0' TERM INT
# Wait in the background so the traps run, and so that this shell, as
# pid 1, reaps what the supervisors leave behind.
while :; do
	sleep 3600 &
	wait $!
done

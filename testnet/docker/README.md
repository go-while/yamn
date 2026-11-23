# Test network in containers

The same test network as in [`..`](..), one node per public server, each in
a container: `yamn-tn-<name>` on `web-<name>`, with podman (the image
name `localhost/yamn-tn` and the `k8s-file` log driver are podman's).
It uses `node.sh` and tnode unchanged; what differs:

- **Ports:** `ports.env` sets mx 25250 and www 28080 for every node
  (`node.sh` reads it; without it, 2525 and 8080 as before). The
  containers use the host's network, so tnode sees the real source
  address of every connection and its allow-list works: only the
  addresses in `nodes.txt` (and loopback) are served. The servers'
  firewalls open the two ports to the nodes only.
- **Image:** no Dockerfile and no registry. `dtestnet.sh image` packs the
  static `yamn` and `tnode` from `../testnet.sh build` (built on a test
  host) with a static busybox (from an Alpine test VM) into a root file
  system tar, which `dtestnet.sh deploy` copies to each server and
  imports as `localhost/yamn-tn:<time>`.
- **Container:** runs as uid 10001 (`yamn-test`), with
  `--memory 256m --cpus 0.5 --pids-limit 128`, the container log capped
  at 10 MB (the node's own logs in its data directory grow: about 15 MB
  in four weeks on the busiest node of the LXC testnet), and
  `--restart unless-stopped`. Its data (keys, pool, Maildirs, logs) is in
  `/srv/yamn-testnet/<name>` on the server, mounted on `~/testnet`; the
  entrypoint runs `node.sh setup`, `init` (first start only) and `start`,
  and `node.sh stop` when the container stops.
- **Mail stays inside:** tnode accepts mail only for `*.yamn.test` and
  sends no bounces; the remailers send only through their own tnode.

## Use

```sh
../testnet.sh build      # static yamn and tnode, on a test host
./dtestnet.sh busybox    # static busybox, from an Alpine test VM
./dtestnet.sh image      # build/yamn-tn.tar
./dtestnet.sh deploy     # load it and (re)start every node's container
./dtestnet.sh keyring    # publish the keys on the hub; then restart the
                         # others (stop, start) so they fetch them now
./dtestnet.sh send 3     # test messages from the hub to its inbox
./dtestnet.sh inbox
./dtestnet.sh status
./dtestnet.sh log NODE [mx|www|yamn|remailer]
./dtestnet.sh clean      # containers, images and /srv/yamn-testnet
```

`HOSTS` (default `web-%s`) maps a node name to its ssh host. A deploy
with a new image keeps each node's data and keys.

On the servers used here (podman 5.4.2, Debian 13), `podman-restart.service`
starts only containers with `--restart always` at boot
(`ExecStart=... start --all --filter restart-policy=always`): after a
server reboot, `dtestnet.sh start` brings the node back.

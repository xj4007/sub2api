#!/usr/bin/env python3
import json
import sys
from pathlib import Path

if len(sys.argv) != 4:
    raise SystemExit("usage: render-wg-config.py <inventory.json> <keys.json> <node-name>")

inventory = json.loads(Path(sys.argv[1]).read_text())
keys = json.loads(Path(sys.argv[2]).read_text())
node_name = sys.argv[3]

node = inventory["nodes"][node_name]
default_listen_port = inventory["listen_port"]
listen_port = node.get("listen_port", default_listen_port)

print("[Interface]")
print(f"Address = {node['wg_ip']}/24")
print(f"ListenPort = {listen_port}")
print("MTU = 1280")
print(f"PrivateKey = {keys[node_name]['private_key']}")

for peer_name, peer in inventory["nodes"].items():
    if peer_name == node_name:
        continue
    peer_listen_port = peer.get("listen_port", default_listen_port)
    print()
    print("[Peer]")
    print(f"PublicKey = {keys[peer_name]['public_key']}")
    print(f"AllowedIPs = {peer['wg_ip']}/32")
    print(f"Endpoint = {peer['public_ip']}:{peer_listen_port}")
    print("PersistentKeepalive = 25")

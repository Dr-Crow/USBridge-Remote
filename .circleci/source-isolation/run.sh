#!/usr/bin/env bash
set -euo pipefail
/usr/bin/python3 - <<'PY'
import json,socket,pathlib
result={'test':'actual source package within Docker internal network','wan_tcp_blocked':False,'physical_hardware':False}
try:
    with socket.create_connection(('1.1.1.1',443),timeout=3):pass
except OSError as e:
    result['wan_tcp_blocked']=True
    result['wan_probe_errno']=e.errno
else:
    raise SystemExit('internal network permitted WAN TCP')
pathlib.Path('/results/network.json').write_text(json.dumps(result,indent=2)+'\n')
PY
export BROKER_SESSION_BINARY=/package/components/bin/broker-session AGENT_SOURCE_BINARY=/package/agent/usbridge-agent
/acceptance/broker-session-tests -test.run '^(TestPortableSessionExecutableMutualTLS|TestPortableAgentBrokerSessionMutualTLS)$' -test.count=5 -test.v | tee /results/broker-mutual-tls.txt
/usr/bin/python3 /acceptance/test_agent_source_lifecycle.py --agent /package/agent/usbridge-agent --streamer /package/components/bin/source-streamer --output /results/lifecycle
/usr/bin/python3 /acceptance/test_agent_source_media.py --agent /package/agent/usbridge-agent --components /package/components --enet-helper /acceptance/enet-control-test-client --output /results/media
/usr/bin/python3 /acceptance/test_agent_source_full_client.py --agent /package/agent/usbridge-agent --streamer /package/components/bin/source-streamer --client /acceptance/full-client --output /results/full-public-client
/usr/bin/python3 /acceptance/test_agent_source_full_client.py --agent /package/agent/usbridge-agent --streamer /package/components/bin/source-streamer --client /acceptance/full-client --output /results/full-public-client-444 --pixel-format yuv444p
/usr/bin/python3 /acceptance/test_agent_source_full_client.py --agent /package/agent/usbridge-agent --streamer /package/components/bin/source-streamer --client /acceptance/full-client --output /results/full-public-client
/usr/bin/python3 /acceptance/test_agent_source_full_client.py --agent /package/agent/usbridge-agent --streamer /package/components/bin/source-streamer --client /acceptance/full-client --output /results/full-public-client-444 --pixel-format yuv444p-input --input-consent
/usr/bin/python3 - <<'PY'
import json,pathlib
out=pathlib.Path('/results');network=json.loads((out/'network.json').read_text());media=json.loads((out/'media/result.json').read_text());lifecycle=json.loads((out/'lifecycle/result.json').read_text())
full=json.loads((out/'full-public-client/result.json').read_text())
chroma444=json.loads((out/'full-public-client-444/result.json').read_text())
assert chroma444['passed'] and chroma444['source_shutdown_joined'] and chroma444['decoded_video_format']['pix_fmt']=='yuv444p'
input_result=json.loads((out/'full-public-client-input/result.json').read_text())
assert input_result['passed'] and input_result['source_shutdown_joined'] and input_result['actual_x11_input_and_disconnect_release_passed']
assert network['wan_tcp_blocked'] and media['passed'] and lifecycle['passed'] and full['passed'] and full['h264_decode_passed'] and full['source_shutdown_joined']
(out/'result.json').write_text(json.dumps({'passed':True,'network':'Docker internal with explicit loopback DNS','real_agent_source_streamer':True,'real_agent_source_broker_mtls':True,'media':media,'full_public_client':full,'full_public_client_444':chroma444,'consented_input':input_result,'wan_tcp_blocked':True,'physical_hardware':False,'usb_os_attached':False,'production_parity':False},indent=2)+'\n')
PY

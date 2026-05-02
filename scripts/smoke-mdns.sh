#!/bin/sh
set -eu

inspect_txt="true"
while [ "$#" -gt 0 ]; do
	case "$1" in
		--inspect-txt)
			inspect_txt="true"
			;;
		-h|--help)
			printf 'usage: %s [--inspect-txt]\n' "$0" >&2
			exit 0
			;;
		*)
			printf 'usage: %s [--inspect-txt]\n' "$0" >&2
			exit 2
			;;
	esac
	shift
done

if [ "$inspect_txt" != "true" ]; then
	printf 'mdns smoke mode is unavailable\n' >&2
	exit 2
fi

output="$(go test ./internal/mdns -run '^TestSmokeTXTInspection$' -count=1 -v)"
printf '%s\n' "$output"

printf '%s\n' "$output" | grep -Fq 'MDNS_SERVICE service=_talka._tcp'
printf '%s\n' "$output" | grep -Fq 'MDNS_TXT pairing=required records=version=1|device_name=Kitchen Speaker|protocol=talka-stream-v1|pairing=required'
printf '%s\n' "$output" | grep -Fq 'MDNS_PAIRING_UPDATE from=required to=paired'
printf '%s\n' "$output" | grep -Fq 'MDNS_TXT pairing=paired records=version=1|device_name=Kitchen Speaker|protocol=talka-stream-v1|pairing=paired'
printf '%s\n' "$output" | grep -Fq 'MDNS_SECRET_SCAN pairing=required status=clear'
printf '%s\n' "$output" | grep -Fq 'MDNS_SECRET_SCAN pairing=paired status=clear'

if printf '%s\n' "$output" | grep -Eiq '(pin|key|token|session|audio|transcript|challenge)'; then
	printf 'forbidden term leaked in mDNS TXT inspection output\n' >&2
	exit 1
fi

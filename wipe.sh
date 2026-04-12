#!/usr/bin/env bash
# PyVikunja - Developed by acidvegas in Python (https://git.acid.vegas)
# vikunja/wipe.sh

[ -f .env ] && source .env || { echo 'error: missing .env file'; exit 1; }

command -v jq >/dev/null || { echo 'error: jq is required (apt install jq)'; exit 1; }

API="${VIKUNJA_URL%/}/api/v1"
AUTH="Authorization: Bearer ${VIKUNJA_TOKEN}"

PROJECTS=(
	'Memory'
	'pyvikunja'
	'home'
)

LABELS=(
	'topic:postgres'
	'topic:docker'
	'topic:auth'
	'topic:deployment'
	'topic:infra'
	'person:alice'
	'person:bob'
	'source:slack'
	'source:ops'
	'source:meeting'
	'kind:fact'
	'kind:decision'
	'kind:preference'
	'kind:reference'
	'bug'
	'feature'
	'refactor'
	'docs'
	'chore'
	'p0'
	'p1'
	'p2'
)

search_ids() {
	local kind="$1"
	local title="$2"

	curl -fsSL -H "$AUTH" -G \
		--data-urlencode "s=$title" \
		--data-urlencode 'per_page=50' \
		"$API/$kind" \
		| jq -r --arg t "$title" '.[]? | select(.title == $t) | .id'
}

api_delete() {
	curl -fsSL -X DELETE -H "$AUTH" "$API$1" >/dev/null
}

wipe_by_title() {
	local kind="$1"
	local title="$2"
	local ids

	ids=$(search_ids "$kind" "$title")

	if [ -z "$ids" ]; then
		echo "    $kind '$title' not found"
		return
	fi

	for id in $ids; do
		if api_delete "/$kind/$id"; then
			echo "    deleted $kind '$title' (id $id)"
		else
			echo "    failed  $kind '$title' (id $id)"
		fi
	done
}

echo '>>> deleting seeded projects (cascades to their tasks)'
for title in "${PROJECTS[@]}"; do
	wipe_by_title 'projects' "$title"
done

echo '>>> deleting seeded labels'
for title in "${LABELS[@]}"; do
	wipe_by_title 'labels' "$title"
done

echo '>>> done'

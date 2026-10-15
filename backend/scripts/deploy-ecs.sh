#!/usr/bin/env bash
# Deploys one image to one ECS Fargate environment:
#   1. registers a new revision of the service's task definition with the new image,
#   2. runs that revision once as `api migrate up` and stops if the migration fails,
#   3. points the service at the new revision and waits until it is stable.
# The service keeps running the previous revision if any step fails.
#
# Called by .github/workflows/backend-deploy-ecs.yml. Required environment:
#   IMAGE                 image to deploy, ideally pinned by digest (repo@sha256:…)
#   ECS_CLUSTER           cluster name
#   ECS_SERVICE           service name
#   ECS_TASK_FAMILY       task definition family the service runs
#   ECS_CONTAINER_NAME    container in that task definition that runs the api binary
#   ECS_SUBNETS           comma-separated subnet ids for the migration task
#   ECS_ASSIGN_PUBLIC_IP  ENABLED when the tasks run in public subnets (optional, default DISABLED)
#   ECS_SECURITY_GROUPS   comma-separated security group ids for the migration task
# Optional: MIGRATION_TIMEOUT_SECONDS (default 1800), STABLE_TIMEOUT_SECONDS (default 1200).
set -euo pipefail

for v in IMAGE ECS_CLUSTER ECS_SERVICE ECS_TASK_FAMILY ECS_CONTAINER_NAME ECS_SUBNETS ECS_SECURITY_GROUPS; do
	if [[ -z "${!v:-}" ]]; then
		echo "::error::$v is not set"
		exit 1
	fi
done
MIGRATION_TIMEOUT_SECONDS="${MIGRATION_TIMEOUT_SECONDS:-1800}"
STABLE_TIMEOUT_SECONDS="${STABLE_TIMEOUT_SECONDS:-1200}"
assign_public_ip="${ECS_ASSIGN_PUBLIC_IP:-DISABLED}"
if [[ "$assign_public_ip" != ENABLED && "$assign_public_ip" != DISABLED ]]; then
	echo "::error::ECS_ASSIGN_PUBLIC_IP must be ENABLED or DISABLED"
	exit 1
fi

csv_json() { jq -cn --arg s "$1" '$s | split(",") | map(gsub("^\\s+|\\s+$"; "")) | map(select(. != ""))'; }

echo "::group::Register task definition"
current="$(aws ecs describe-task-definition --task-definition "$ECS_TASK_FAMILY" --query taskDefinition --output json)"
if ! jq -e --arg name "$ECS_CONTAINER_NAME" 'any(.containerDefinitions[]; .name == $name)' <<<"$current" >/dev/null; then
	echo "::error::Task definition $ECS_TASK_FAMILY has no container named $ECS_CONTAINER_NAME"
	exit 1
fi
next="$(jq -c --arg name "$ECS_CONTAINER_NAME" --arg image "$IMAGE" '
	.containerDefinitions |= map(if .name == $name then .image = $image else . end)
	| del(.taskDefinitionArn, .revision, .status, .requiresAttributes, .compatibilities, .registeredAt, .registeredBy, .deregisteredAt)
' <<<"$current")"
task_def_arn="$(aws ecs register-task-definition --cli-input-json "$next" --query taskDefinition.taskDefinitionArn --output text)"
echo "Registered $task_def_arn"
echo "::endgroup::"

echo "::group::Run migrations"
network="$(jq -cn --argjson subnets "$(csv_json "$ECS_SUBNETS")" --argjson sgs "$(csv_json "$ECS_SECURITY_GROUPS")" \
	--arg public "$assign_public_ip" \
	'{awsvpcConfiguration: {subnets: $subnets, securityGroups: $sgs, assignPublicIp: $public}}')"
overrides="$(jq -cn --arg name "$ECS_CONTAINER_NAME" '{containerOverrides: [{name: $name, command: ["migrate", "up"]}]}')"
run="$(aws ecs run-task --cluster "$ECS_CLUSTER" --task-definition "$task_def_arn" --launch-type FARGATE \
	--network-configuration "$network" --overrides "$overrides" --started-by github-actions --output json)"
task_arn="$(jq -r '.tasks[0].taskArn // empty' <<<"$run")"
if [[ -z "$task_arn" ]]; then
	echo "::error::Could not start the migration task: $(jq -c '.failures' <<<"$run")"
	exit 1
fi
echo "Started $task_arn"
deadline=$((SECONDS + MIGRATION_TIMEOUT_SECONDS))
while :; do
	status="$(aws ecs describe-tasks --cluster "$ECS_CLUSTER" --tasks "$task_arn" --query 'tasks[0].lastStatus' --output text)"
	[[ "$status" == "STOPPED" ]] && break
	if ((SECONDS >= deadline)); then
		echo "::error::Migration task still $status after ${MIGRATION_TIMEOUT_SECONDS}s; stopping it. The service was not changed."
		aws ecs stop-task --cluster "$ECS_CLUSTER" --task "$task_arn" --reason "deploy timed out" >/dev/null
		exit 1
	fi
	sleep "${POLL_SECONDS:-10}"
done
task="$(aws ecs describe-tasks --cluster "$ECS_CLUSTER" --tasks "$task_arn" --output json)"
exit_code="$(jq -r --arg name "$ECS_CONTAINER_NAME" '.tasks[0].containers[] | select(.name == $name) | .exitCode // "none"' <<<"$task")"
if [[ "$exit_code" != "0" ]]; then
	echo "::error::Migration failed (exit code $exit_code, $(jq -r '.tasks[0].stoppedReason // "no reason given"' <<<"$task")). Check the task's logs in CloudWatch. The service was not changed."
	exit 1
fi
echo "Migrations applied"
echo "::endgroup::"

echo "::group::Roll out service"
aws ecs update-service --cluster "$ECS_CLUSTER" --service "$ECS_SERVICE" --task-definition "$task_def_arn" --query 'service.serviceName' --output text
deadline=$((SECONDS + STABLE_TIMEOUT_SECONDS))
while :; do
	# shellcheck disable=SC2016 # backticks are a JMESPath literal
	primary="$(aws ecs describe-services --cluster "$ECS_CLUSTER" --services "$ECS_SERVICE" \
		--query 'services[0].deployments[?status==`PRIMARY`] | [0]' --output json)"
	primary_td="$(jq -r '.taskDefinition' <<<"$primary")"
	state="$(jq -r '.rolloutState // "UNKNOWN"' <<<"$primary")"
	if [[ "$primary_td" != "$task_def_arn" ]]; then
		echo "::error::The service rolled back to $primary_td (deployment circuit breaker). Check the service events and task logs."
		exit 1
	fi
	case "$state" in
	COMPLETED) break ;;
	FAILED)
		echo "::error::Rollout failed: $(jq -r '.rolloutStateReason // ""' <<<"$primary")"
		exit 1
		;;
	esac
	if ((SECONDS >= deadline)); then
		echo "::error::Service not stable after ${STABLE_TIMEOUT_SECONDS}s (rollout state $state)."
		exit 1
	fi
	sleep "${POLL_SECONDS:-15}"
done
echo "Service $ECS_SERVICE is running $task_def_arn"
echo "::endgroup::"

if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
	{
		echo "### Deployed to $ECS_SERVICE"
		echo "- Image: \`$IMAGE\`"
		echo "- Task definition: \`$task_def_arn\`"
		echo "- Migrations: applied by \`$task_arn\`"
	} >>"$GITHUB_STEP_SUMMARY"
fi

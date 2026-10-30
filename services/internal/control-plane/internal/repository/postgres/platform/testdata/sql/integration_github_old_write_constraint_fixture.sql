-- Только disposable clone: прежняя граница не считается новым автономным admission.
ALTER TABLE control_plane.integration_invocations DROP CONSTRAINT integration_invocations_approval_check;
ALTER TABLE control_plane.integration_invocations ADD CONSTRAINT integration_invocations_approval_check CHECK (
        (risk='READ' AND approval_policy='NONE' AND (state<>'WAITING_APPROVAL' OR mailbox_gate_required)) OR
        (risk IN ('WRITE','SENSITIVE','DESTRUCTIVE') AND approval_policy IN ('HUMAN_EACH_EFFECT','HUMAN_SCOPED')) OR
        (risk IN ('WRITE','SENSITIVE','DESTRUCTIVE') AND approval_policy='NONE' AND resource_kind='EMAIL_SENDER'
            AND (state<>'WAITING_APPROVAL' OR mailbox_gate_required)) OR
        (risk='WRITE' AND approval_policy='NONE' AND resource_kind='GITHUB_REPOSITORY' AND operation=capability_key
         AND operation IN ('github.issue.comment.create','github.issue.comment.update','github.pull_request.create',
                           'github.pull_request.update','github.pull_request.review.create')
         AND NOT mailbox_gate_required AND state<>'WAITING_APPROVAL')
    );

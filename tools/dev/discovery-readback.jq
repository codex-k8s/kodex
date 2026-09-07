def account_ref: type == "string" and test("^pacc_[A-Za-z0-9_-]{8,88}$");
def valid_ref: type == "string" and test("^[A-Za-z0-9_-]{8,96}$");
def valid_state:
  .version == 1 and
  (.refs.coordinatorProviderAccountRef | account_ref) and
  (.refs.analystProviderAccountRef | account_ref) and
  .refs.coordinatorProviderAccountRef != .refs.analystProviderAccountRef and
  ([.refs.firstRunRef, .refs.continuationRunRef, .refs.workflowRunRef,
    .refs.scheduledRunRef, .refs.instructionRunRef] |
    length == 5 and all(.[]; valid_ref) and (unique | length) == 5) and
  (.refs.publishedInstructionRef | valid_ref);
def expected:
  .refs as $r | {
    ($r.firstRunRef): $r.coordinatorProviderAccountRef,
    ($r.continuationRunRef): $r.coordinatorProviderAccountRef,
    ($r.instructionRunRef): $r.coordinatorProviderAccountRef,
    ($r.workflowRunRef): $r.coordinatorProviderAccountRef,
    ($r.scheduledRunRef): $r.analystProviderAccountRef
  };
if ($state | valid_state | not) then error("invalid discovery fixture state")
elif $mode == "state" then $state
elif $mode == "expected" then $state | expected
elif $mode == "readback" then
  .run_accounts == ($state | expected) and
  (.selected_accounts | keys | sort) == ([$state.refs.coordinatorProviderAccountRef, $state.refs.analystProviderAccountRef] | sort) and
  (.selected_accounts | all(.[]; type == "string" and test("^[a-z][a-z0-9_-]{1,95}$"))) and
  .instruction_runtime_count >= 1 and .instruction_runtime_valid == true
else error("unsupported discovery readback mode") end

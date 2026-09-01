def unique_sorted:
  sort | unique;

def rows_by_run:
  reduce .[] as $row ({}; .[$row.run_ref] = ((.[$row.run_ref] // []) + [$row]));

. as $rows
| ($rows | rows_by_run) as $rows_by_run
| ([
    $expected_runs
    | to_entries[]
    | . as $expected
    | ($rows_by_run[$expected.key] // []) as $matches
    | if ($matches | length) == 0 then
        "run \($expected.key) is absent from the database readback"
      elif ($matches | length) != 1 then
        "run \($expected.key) has a non-unique database readback"
      else
        $matches[0] as $row
        | [
            if $row.found != true then
              "run \($expected.key) was not found"
            else empty end,
            if ($row.session_ref | type) != "string" or ($row.session_ref | length) == 0 then
              "run \($expected.key) has no logical session"
            else empty end,
            if $row.session_account_ref != $expected.value then
              "run \($expected.key) uses session account ref \($row.session_account_ref) instead of \($expected.value)"
            else empty end,
            if ($row.runtime_revision_count | type) != "number" or $row.runtime_revision_count < 1 then
              "run \($expected.key) has no materialized runtime revision"
            else empty end,
            if $row.runtime_boundary_consistent != true then
              "run \($expected.key) runtime revisions cross the run session or organization boundary"
            else empty end,
            if (($row.runtime_account_refs // []) | unique_sorted) != [$expected.value] then
              "run \($expected.key) runtime revisions do not exclusively use account ref \($expected.value)"
            else empty end
          ][]
      end
  ]
  + [
      $same_sessions[]
      | . as $pair
      | ($rows_by_run[$pair.original] // []) as $original_matches
      | ($rows_by_run[$pair.continuation] // []) as $continuation_matches
      | if ($original_matches | length) != 1 or ($continuation_matches | length) != 1 then
          "session affinity cannot be checked for \($pair.original) and \($pair.continuation)"
        elif $original_matches[0].session_ref != $continuation_matches[0].session_ref then
          "continuation \($pair.continuation) changed logical session from \($original_matches[0].session_ref) to \($continuation_matches[0].session_ref)"
        elif $original_matches[0].session_account_ref != $continuation_matches[0].session_account_ref then
          "continuation \($pair.continuation) changed provider account within logical session"
        else empty end
    ]) as $errors_before_distinct
| ([
    $expected_runs
    | keys[] as $run_ref
    | ($rows_by_run[$run_ref] // [])
    | select(length == 1)
    | .[0]
    | select(.found == true)
    | .session_account_ref
    | select(type == "string" and length > 0)
  ] | unique_sorted) as $observed_account_refs
| ($errors_before_distinct
   + if ($observed_account_refs | length) < $required_distinct_accounts then
       ["expected at least \($required_distinct_accounts) distinct provider account refs, observed \($observed_account_refs | length)"]
     else [] end) as $errors
| {
    ok: (($errors | length) == 0),
    checked_runs: ($expected_runs | length),
    checked_session_pairs: ($same_sessions | length),
    observed_account_refs: $observed_account_refs,
    errors: $errors
  }

def valid_running_pod:
  (.status.containerStatuses // []) as $statuses |
  (.spec.containers // []) as $containers |
  ($containers | length) > 0 and
  ($statuses | length) == ($containers | length) and
  all($containers[];
    . as $container |
    any($statuses[];
      .name == $container.name and
      .ready == true and
      ((.imageID // "") | test("@sha256:[a-f0-9]{64}$")) and
      ((.imageID // "") | endswith("@sha256:0000000000000000000000000000000000000000000000000000000000000000") | not) and
      (if ($container.image | startswith("localhost:5001/mattercodex/")) then
         ($container.image | test("@sha256:[a-f0-9]{64}$")) and
         ((.imageID // "") | endswith("@" + ($container.image | split("@") | last)))
       else
         true
       end)
    )
  );

[.items[] | select(.status.phase == "Running")] as $running |
($running | length) > 0 and all($running[]; valid_running_pod)

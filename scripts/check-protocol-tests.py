"""Fail closed if a protocol selector matched zero tests or any test skipped."""
import json
import sys

ran, passed, skipped, failed = set(), set(), set(), set()
with open(sys.argv[1], encoding="utf-8") as log:
    for line in log:
        event = json.loads(line)
        test = event.get("Test")
        if not test:
            continue
        action = event.get("Action")
        if action == "skip":
            skipped.add(test)
        elif action == "fail":
            failed.add(test)
        if "/" not in test:
            if action == "run":
                ran.add(test)
            elif action == "pass":
                passed.add(test)
if not ran or ran != passed or skipped or failed:
    sys.exit(f"Invalid protocol entry: ran={ran}, passed={passed}, skipped={skipped}, failed={failed}")
print(f"Verified {len(passed)} real passing protocol tests: {', '.join(sorted(passed))}")

#!/bin/zsh
set -e

if [ -z "$1" ]; then
  echo "Usage: $0 <iterations>"
  exit 1
fi

prompt=$(
  cat <<'EOF'
@plan.md @tasks.md @progress.md @research.md
You did the research of research.md, to to implement the product described in plan.md for which tasks were defined in @tasks.md.
Now it's time to implement this task by task following these instructions:
1. Find the highest-priority feature in tasks.md to work on and work only on that feature.
This should be the one YOU decide has the highest priority - not necessarily the first in the list
2. Write tests for the acceptance criteria of the feature that are noted in tasks.md and check that tests pass before you decide the criteria is done
3. If you managed to finish acceptance criteria than update tasks.md checking of the acceptance criteria by putting an x in the markdow checkbox.
4. We are running this in a loop where you work on 1 task per iteration. However you can use @progress.md to leave messages to the future you. If you don't succeed with a task or need to let a future you know something you can use this. However don't leave bloat.
Use this to leave a note for the next person working in the codebase.
However don't leave useless bloat that confuses that person.
5. Make a git commit of the feature with the feature you worked on in the name. However don't push to origin.
6. If you notice that tasks.md is complete (all the acceptance criteria of all features are complete) output <promise>COMPLETE</promise>. Under no other circumstance even in your reasoning mention <promise>COMPLETE</promise> even when you want to mention what no to do. So don't say things like "Not all tasks are complete yet, so I did not output `<promise>COMPLETE</promise>`." 
EOF
)

for ((i = 1; i <= $1; i++)); do
  echo "Iteration $i"
  echo "-------------"
  result=$(codex exec --config model="gpt-5.3-codex" --config model_reasoning_effort="high" --yolo "${prompt}")
  echo "$result"
  if [[ "$result" == *"<promise>COMPLETE</promise>"* ]]; then
    echo "PRD complete, exiting."
    exit 0
  fi
done

#!/usr/bin/env bash
set -euo pipefail
if [[ -n "${USBRIDGE_CI_ROOT:-}" ]]; then cd "$(cygpath -u "$USBRIDGE_CI_ROOT")"; fi
export PATH="/ucrt64/bin:/usr/bin:$PATH"
[[ "$(git rev-parse HEAD)" == "${CIRCLE_SHA1:?exact commit required}" ]]
python3 -c 'import os; assert os.name == "nt"'
mkdir -p artifacts/windows-manager-staging
PYTHONPATH=.circleci/source python3 -m unittest discover -s .circleci/source -p test_windows_manager_staging_diagnostics.py
python3 .circleci/source/run_windows_manager_staging_tests.py --output "$(cygpath -m "$PWD/artifacts/windows-manager-staging/result.json")" --commit "$CIRCLE_SHA1"

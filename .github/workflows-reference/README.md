# Inactive upstream workflow reference

The original GitHub Actions files are retained here for migration reference.
GitHub only discovers workflow YAML in `.github/workflows`, so these do not run
on this branch. CircleCI builds the Windows test agent via `.circleci/config.yml`.
No tag/release, website deployment, translation or signing jobs are enabled by
this migration. Restore a workflow deliberately if that behavior is wanted.

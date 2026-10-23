set -eu
BUDGET_TOOLS=/tools sh tests/scheduler/build-budget.sh
go version -m /tools/budget-pg.test
go version -m /tools/budget-pg-race.test

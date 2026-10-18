.PHONY: install frontend build run check test-integration db dev

# Compatibility shortcuts. Bazel owns compilation, dependencies and checks.
install:
	./bazel fetch //...

frontend:
	./bazel build //:frontend

build:
	./bazel run //:export

db:
	./bazel run //:db

run:
	ACTA_DATABASE_URL="$${ACTA_DATABASE_URL:-$(DEV_DATABASE_URL)}" ./bazel run //:run

check:
	./bazel test //...
	./bazel run //:format-check
	./bazel run //:gazelle -- -mode=diff

test-integration:
	./bazel run //:integration

dev:
	./bazel run //:dev

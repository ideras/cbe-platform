---
title: Laboratorio de carga – Cálculo de estadísticas
duration: 2h
grace: 2m
image: cbe/cpp20
terminal: on
run: make run
check: make check
submit: [src/**, include/**, Makefile]
ignore: [build/**, **/*.o]
points: 30
---

This is the project of the Phase 8 load test (`deploy/workspace/test-load.sh`): eight source files that take a couple of seconds each to build with
`-O2`, the way a lab with templates does. Build with `make`; `make check` runs the checks.

[ ] Read the units in `src/` {#t1 points=10}
[ ] Change one of them and build again {#t2 points=10}
[ ] Run `make check` {#t3 points=10}

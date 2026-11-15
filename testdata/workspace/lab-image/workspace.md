---
title: Laboratory – a library the course image does not have
duration: 1h
grace: 2m
image: cbe/cpp20
terminal: on
run: make run
check: make check
submit: [src/**, Makefile]
ignore: [build/**, **/*.o]
points: 10
---

This exam has its own image: `libgreet` (the header `greet.hpp` and the library `-lgreet`) is installed in it, and in no other.
Make `src/main.cpp` print the greeting that `libgreet` gives for your name, then run it with `make run`.

[ ] `make run` prints `hello, <your name>` using `libgreet` {#t1 points=10}

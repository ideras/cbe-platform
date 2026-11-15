build/main: src/main.cpp
	@mkdir -p build
	g++ -std=c++20 -Wall -o $@ src/main.cpp -lgreet

.PHONY: run check clean
run: build/main
	./build/main

check: build/main
	./build/main | grep -q '^hello, '

clean:
	rm -rf build

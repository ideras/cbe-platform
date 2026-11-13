CXX      := g++
CXXFLAGS := -std=c++20 -Wall -Wextra -O0 -Iinclude

build/list_demo: src/main.cpp src/list.cpp include/list.hpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -o $@ src/main.cpp src/list.cpp

build/sample_test: tests/sample_test.cpp src/list.cpp include/list.hpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -o $@ tests/sample_test.cpp src/list.cpp

.PHONY: run check clean
run: build/list_demo
	./build/list_demo

check: build/sample_test
	./build/sample_test

clean:
	rm -rf build

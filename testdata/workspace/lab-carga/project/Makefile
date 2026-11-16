CXX      := g++
CXXFLAGS := -std=c++20 -O2 -Wall -Iinclude
SRC      := $(wildcard src/*.cpp)
OBJ      := $(SRC:src/%.cpp=build/%.o)

all: build/app

build/app: $(OBJ)
	$(CXX) $(CXXFLAGS) -o $@ $(OBJ)

build/%.o: src/%.cpp include/units.hpp | build
	$(CXX) $(CXXFLAGS) -c $< -o $@

build:
	mkdir -p build

run: build/app
	./build/app

check: build/app
	./build/app --check

clean:
	rm -rf build

.PHONY: all run check clean

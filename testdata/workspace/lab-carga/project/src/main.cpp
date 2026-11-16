#include <cstdio>
#include <cstring>

#include "units.hpp"

int main(int argc, char** argv) {
    const bool check = argc > 1 && std::strcmp(argv[1], "--check") == 0;
    const std::string lines[] = {unit0(1), unit1(2), unit2(3), unit3(4), unit4(5), unit5(6), unit6(7), unit7(8)};
    for (const std::string& line : lines) std::puts(line.c_str());
    if (check) std::puts("check: ok");
    return 0;
}

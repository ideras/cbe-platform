// The hidden driver of lab-listas (HIDDEN-CASES-MARKER-4f1c9a: it never reaches a student). It runs the cases of one operation against the
// student's List and prints "passed total" on standard output; what each case did goes to standard error.
// Usage: driver OPERATION CASES-FILE
#include <fstream>
#include <iostream>
#include <sstream>
#include <string>
#include <vector>

#include "list.hpp"

int main(int argc, char** argv) {
    if (argc != 3) {
        std::cerr << "usage: driver OPERATION CASES-FILE\n";
        return 2;
    }
    const std::string wanted = argv[1];
    std::ifstream cases(argv[2]);
    std::string line;
    int total = 0;
    int passed = 0;
    while (std::getline(cases, line)) {
        std::istringstream in(line);
        std::string operation;
        in >> operation;
        if (operation != wanted) continue;
        std::vector<int> values;
        std::vector<int> expected;
        std::string word;
        bool after_arrow = false;
        while (in >> word) {
            if (word == "->") {
                after_arrow = true;
            } else {
                (after_arrow ? expected : values).push_back(std::stoi(word));
            }
        }
        List list;
        for (int value : values) {
            if (operation == "push_front") list.push_front(value);
            else list.insert_sorted(value);
        }
        std::vector<int> got;
        for (const Node* node = list.head(); node != nullptr; node = node->next) got.push_back(node->value);
        ++total;
        if (got == expected && list.size() == static_cast<int>(expected.size())) {
            ++passed;
            std::cerr << "ok: " << line << '\n';
        } else {
            std::cerr << "FAILED: " << line << '\n';
        }
    }
    std::cout << passed << ' ' << total << '\n';
    return 0;
}

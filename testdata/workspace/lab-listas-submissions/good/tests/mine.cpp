#include <cstdio>

#include "list.hpp"

int main() {
    List list;
    list.push_front(2);
    list.push_front(1);
    list.insert_sorted(3);
    if (list.size() != 3 || list.head()->value != 1) {
        std::puts("mine: FAILED");
        return 1;
    }
    std::puts("mine: ok");
    return 0;
}

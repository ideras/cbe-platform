#include <cstdio>

#include "list.hpp"

int main() {
    List list;
    list.push_front(7);
    if (list.size() != 1 || list.head() == nullptr || list.head()->value != 7) {
        std::puts("push_front: FAILED");
        return 1;
    }
    std::puts("push_front: ok");
    return 0;
}

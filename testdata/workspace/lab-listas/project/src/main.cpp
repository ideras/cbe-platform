#include <iostream>

#include "list.hpp"

int main() {
    List list;
    list.push_front(3);
    list.push_front(1);
    list.insert_sorted(2);
    for (const Node* node = list.head(); node != nullptr; node = node->next) {
        std::cout << node->value << ' ';
    }
    std::cout << '\n';
    return 0;
}

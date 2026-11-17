#include "list.hpp"

List::~List() {
    while (head_ != nullptr) {
        Node* next = head_->next;
        delete head_;
        head_ = next;
    }
}

void List::push_front(int value) {
    // TODO: put a new node with `value` at the front of the list.
    (void)value;
}

void List::insert_sorted(int value) {
    // TODO: insert a new node so the list stays in ascending order.
    (void)value;
}

int List::size() const {
    int count = 0;
    for (const Node* node = head_; node != nullptr; node = node->next) {
        ++count;
    }
    return count;
}

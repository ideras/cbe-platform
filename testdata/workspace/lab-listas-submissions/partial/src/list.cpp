#include "list.hpp"

List::~List() {
    while (head_ != nullptr) {
        Node* next = head_->next;
        delete head_;
        head_ = next;
    }
}

void List::push_front(int value) {
    head_ = new Node{value, head_};
}

void List::insert_sorted(int value) {
    // Not sorted yet: the value goes to the front.
    head_ = new Node{value, head_};
}

int List::size() const {
    int count = 0;
    for (const Node* node = head_; node != nullptr; node = node->next) {
        ++count;
    }
    return count;
}

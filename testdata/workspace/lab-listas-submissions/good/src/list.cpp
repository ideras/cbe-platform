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
    Node** link = &head_;
    while (*link != nullptr && (*link)->value < value) link = &(*link)->next;
    *link = new Node{value, *link};
}

int List::size() const {
    int count = 0;
    for (const Node* node = head_; node != nullptr; node = node->next) {
        ++count;
    }
    return count;
}

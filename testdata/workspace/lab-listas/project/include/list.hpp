#pragma once

// A singly linked list of ints.
struct Node {
    int value;
    Node* next;
};

class List {
public:
    List() = default;
    List(const List&) = delete;
    List& operator=(const List&) = delete;
    ~List();

    void push_front(int value);      // TODO t1
    void insert_sorted(int value);   // TODO t2: keeps the list in ascending order
    int size() const;
    const Node* head() const { return head_; }

private:
    Node* head_ = nullptr;
};

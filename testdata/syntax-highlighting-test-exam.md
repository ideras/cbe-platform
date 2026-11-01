---
title: Syntax highlighting test
duration: 60m
min_volume: 0
---

### Instructions

This exam is only for checking the code editor of the student application. Nothing in it is graded.

Each exercise has an answer box that already holds sample code in one language: C, C++, Python and MIPS32. Look at the sample, then click in the box and type a few lines of your own at the end.

For every editor check that:

- the language name is shown in the top right corner of the box;
- **keywords** (`for`, `def`, `la`, …) are bold and dark blue;
- **types**, preprocessor lines (`#include`), MIPS directives (`.data`) and Python decorators (`@dataclass`) are dark blue but not bold;
- **strings**, character literals and `<header>` names are olive green;
- **numbers** and the constants `NULL`, `nullptr`, `true`, `None`, `True` and `False` are rust red;
- **comments** are grey and italic;
- **variable names** and MIPS registers (`$t0`) are amber;
- **function names** and labels are teal, while method names after a dot (`items_.push_back`) and words the editor does not know are left in the normal text colour;
- no completion popup, hint or error mark ever appears while you type;
- a read-only code block in a statement (like the one in each exercise) has no colours at all: only answer boxes are highlighted.

## Exercise 1 — C

Complete `count_vowels` so that it returns how many vowels `text` has. The signature below is read-only code, so it stays plain:

```c
int count_vowels(const char *text);
```

Things to look for: `#include` and `#define` lines, both kinds of comment, the `'a'` characters, the hexadecimal and floating point numbers, `NULL`, `struct`, `enum`, `switch`, `goto` and the label `done:`, and the names `count_vowels`, `main` and `printf`.

At the end of the box type `    pri` and confirm that no popup offers `printf`.

```answer {#code lang=c lines=48}
#include <stdio.h>
#include <string.h>
#define MAX_LEN 64

/* A block comment
   that spans two lines. */
typedef struct {
    char letter;
    int count;
} Tally;

enum { VOWELS = 5, FLAG = 0x1F, RATIO = 2 };

// TODO: finish this function (a line comment)
static int count_vowels(const char *text)
{
    int total = 0;
    unsigned long mask = 0xFFUL;
    double scale = 3.14e-2;
    char *spare = NULL;

    for (size_t i = 0; text[i] != '\0'; i++) {
        switch (text[i]) {
        case 'a': case 'e': case 'i': case 'o': case 'u':
            total++;
            break;
        default:
            break;
        }
    }
    if (total > VOWELS * RATIO) goto done;
    total += (int)(mask & 0x7) + (int)scale;
done:
    return total;
}

int main(void)
{
    char word[MAX_LEN] = "programming\n";
    printf("%s has %d vowels\n", word, count_vowels(word));
    return 0;
}
```

## Exercise 2 — C++

Write `exam::Stack<T>` so that the words come out in reverse order. This read-only declaration stays plain:

```cpp
template <typename T> class Stack;
```

Things to look for: `template`, `typename`, `class`, `namespace`, `using`, `auto`, `constexpr`, `noexcept`, `nullptr`, `public:` and `private:`, the types `std::string` and `std::vector`, the string literals with the `s` suffix, the character literal `'\n'` and both kinds of comment. The method names `push_back`, `pop_back` and `empty` stay plain.

At the end of the box type `    std::co` and confirm that no popup offers `std::cout`.

```answer {#code lang="c++" lines=46}
#include <iostream>
#include <string>
#include <vector>

namespace exam {

// A small generic stack (a line comment).
template <typename T>
class Stack {
public:
    void push(const T &value) { items_.push_back(value); }
    T pop() {
        T top = std::move(items_.back());
        items_.pop_back();
        return top;
    }
    [[nodiscard]] bool empty() const noexcept { return items_.empty(); }

private:
    std::vector<T> items_;
};

}  // namespace exam

/* TODO: print the words in reverse order. */
int main() {
    using namespace std::string_literals;
    exam::Stack<std::string> words;
    for (auto word : {"one"s, "two"s, "three"s}) {
        words.push(word);
    }
    auto shout = [](const std::string &s) { return s + "!"; };
    while (!words.empty()) {
        std::cout << shout(words.pop()) << '\n';
    }
    int *ptr = nullptr;
    bool done = true;
    constexpr double pi = 3.14159;
    return done && ptr == nullptr ? 0 : 1;
}
```

## Exercise 3 — Python

Complete `describe` so that it returns a one-line description of a student. This read-only signature stays plain:

```python
def describe(student, *, verbose=False): ...
```

Things to look for: `import`, `from`, `class`, `def`, `lambda`, `yield`, `try`, `except` and `finally`, the decorator `@dataclass`, the docstring and the f-string, the numbers in different bases (`0x1F`, `0b101`, `1_000`, `2.5e-3`), `None`, `True` and `False`, and the `#` comments.

Expected quirk: the standard Python grammar also colours `*` (as in `*evens`) and the format spec `.1f` in the f-string as keywords (bold blue).

At the end of the box type `    pri` and confirm that no popup offers `print`.

```answer {#code lang=python lines=50}
import math
from dataclasses import dataclass

LIMIT = 10          # a module constant
THRESHOLD = 0x1F + 0b101 + 1_000 + 2.5e-3


@dataclass
class Student:
    """A student record (a triple-quoted docstring)."""
    name: str
    grades: list

    def average(self) -> float:
        if not self.grades:
            return 0.0
        return sum(self.grades) / len(self.grades)


def describe(student, *, verbose=False):
    # TODO: return a one-line description
    label = f"{student.name}: {student.average():.1f}"
    if verbose and student.average() >= 60 and student is not None:
        label += ' (passing)'
    return label


def squares(limit):
    for n in range(limit):
        yield n ** 2


if __name__ == "__main__":
    ada = Student("Ada", [90, 85, 100])
    print(describe(ada, verbose=True))
    evens = [n for n in squares(LIMIT) if n % 2 == 0]
    lookup = {k: v for k, v in zip("abc", evens)}
    try:
        total = lambda a, b: a + b
        print(total(*evens[:2]), lookup, None, True, False)
    except (ValueError, KeyError) as error:
        raise RuntimeError("failed") from error
    finally:
        pass
```

## Exercise 4 — MIPS32

Write the loop of `sum_to_n`, which leaves `1 + 2 + ... + $a0` in `$v0`. This read-only line stays plain:

```mips
sum_to_n:   # $a0 = n, result in $v0
```

Things to look for: the directives (`.data`, `.word`, `.asciiz`, `.globl`), the labels (`main:`, `loop:`), the registers (`$t0`, `$sp`, `$v0`), the instructions (`la`, `lw`, `jal`, `bgt`, `syscall` and the floating point ones such as `add.s`), the decimal and hexadecimal numbers, the string and character literals and the `#` comments.

The line starting with `frobnicate` is not a MIPS instruction, so it must stay plain (only its register and its comment are coloured).

At the end of the box type `        ad` and confirm that no popup offers `add`.

```answer {#code lang=mips lines=48}
# Sum the integers 1..n (a line comment)
        .data
n:      .word   10
msg:    .asciiz "The sum is: "
nl:     .asciiz "\n"
vals:   .byte   0x0A, 'x', 255
half:   .float  3.14

        .text
        .globl  main
main:
        la      $t0, n              # load the address of n
        lw      $a0, 0($t0)         # $a0 = n
        jal     sum_to_n
        move    $s0, $v0            # keep the result

        li      $v0, 4              # print_string
        la      $a0, msg
        syscall
        li      $v0, 1              # print_int
        move    $a0, $s0
        syscall
        li      $v0, 10             # exit
        syscall

sum_to_n:                           # TODO: write the loop below
        addiu   $sp, $sp, -8
        sw      $ra, 4($sp)
        li      $v0, 0
        li      $t1, 1
loop:   bgt     $t1, $a0, done
        add     $v0, $v0, $t1
        addi    $t1, $t1, 1
        j       loop
done:   lw      $ra, 4($sp)
        addiu   $sp, $sp, 8
        jr      $ra

        lwc1    $f0, half($zero)
        add.s   $f2, $f0, $f0
        c.lt.s  $f0, $f2
        frobnicate $t0              # not an instruction: stays plain
```

## Exercise 5 — Java (control, plain text)

Java is not one of the supported languages, so this box must show **no colours at all**, only the language name `java` in the corner. It is here to prove that an unknown language stays plain.

```answer {#code lang=java lines=10}
public class Plain {
    // Not highlighted: this language has no parser.
    public static void main(String[] args) {
        int answer = 42;
        System.out.println("plain text " + answer);
    }
}
```

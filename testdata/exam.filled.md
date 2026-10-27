---
exam: "Examen I Parcial – Organización de Computadoras"
class: "CCC403 Sección 1"
student: "José Ñúñez Martínez"
account_number: "20231234"
status: "submitted by the student"
submitted_at: "2026-10-20 10:12:31 -06:00"
answer_version: 14
downloaded_at: "2026-10-21 08:00:05 -06:00"
---

### Instrucciones Generales

- **Duración:** 1 hora 20 minutos.
- Para los ejercicios de traducción a MIPS32, siga las convenciones de llamada estándar de MIPS (`$a0`–`$a3` para argumentos, `$v0` para retorno, `$ra` para dirección de retorno, `$sp` para el stack).
- Utilice únicamente las instrucciones vistas en clase: `add`, `sub`, `addi`, `and`, `or`, `sll`, `srl`, `mult`, `div`, `mflo`, `mfhi`, `lw`, `sw`, `lb`, `lbu`, `sb`, `beq`, `bne`, `slt`, `slti`, `j`, `jr`, `jal`.
- **No está permitido el uso de pseudo-instrucciones** como `blt`, `bgt`, `move`, `li`, etc. Debe expandirlas a su forma real.
- Puede usar `$t0`–`$t9` y `$s0`–`$s7` libremente, respetando la convención caller/callee save.
- Todas las variables enteras (`int`) son de **32 bits (4 bytes)**.

## Ejercicio 1 — Análisis de Rendimiento {points=20}

Considere el siguiente programa en pseudoensamblador que calcula la suma de cuadrados:

```
main:
        $v0 = 0              // sum = 0
        $v1 = 5              // n = 5
        $a0 = 0              // i = 0

loop:
        $a1 = $a0 * $a0      // i * i
        $v0 = $v0 + $a1      // sum += i * i
        $a0 = $a0 + 1        // i++
        $t0 = ($a0 < $v1)    // i < n ?
        if ($t0 == 1) then goto loop

        // resultado en $v0
        goto $ra
```

### a. Conteo de Instrucciones {points=10}

Realice la traza del programa y desglose la cantidad de instrucciones ejecutadas por categoría:

| Categoría                       | Cantidad   |
|---------------------------------|:----------:|
| ALU / Escritura de registros    | `16`    |
| Multiplicación                  | `5`   |
| Saltos (branches)               | `5` |
| Saltos incondicionales (jumps)  | `1`   |
| **Total**                       | `27`  |

Escriba la traza o el procedimiento que lo llevó a esos números:

```text title="1a.trace"
i=0: $a1=0  $v0=0   $a0=1  $t0=1
i=1: $a1=1  $v0=1   $a0=2  $t0=1
i=2: $a1=4  $v0=5   $a0=3  $t0=1
i=3: $a1=9  $v0=14  $a0=4  $t0=1
i=4: $a1=16 $v0=30  $a0=5  $t0=0
```

### b. Cálculo del CPI {points=10}

Tomando en cuenta la siguiente clasificación de ciclos por instrucción:

- ALU / Escritura de registros → 1 ciclo
- Multiplicación → 2 ciclos
- Saltos (branches) → 2 ciclos
- Saltos incondicionales (jump) → 2 ciclos

Calcule:

1. **(3 pts)** Los ciclos totales del programa: `44`
2. **(4 pts)** El **CPI** (ciclos por instrucción) del programa: `1.63`
3. **(3 pts)** Si se rediseña el multiplicador para que `mult` tarde solo 1 ciclo, ¿cuál sería el nuevo CPI y la **mejora (speedup)** respecto al CPI original?
   Nuevo CPI: `1.44` — Speedup: `1.13`

Muestre sus cálculos:

```text title="1b.work"
16 ALU + 5 mult + 5 branches + 1 jump = 27 instrucciones
Ciclos: 16·1 + 5·2 + 5·2 + 1·2 = 44
CPI = 44 / 27 ≈ 1.63
```

## Ejercicio 2 — Traducción de Fragmentos Cortos {points=15}

Traduzca cada uno de los siguientes fragmentos de C a MIPS32. **No use branches, jumps ni procedimientos.** Suponga que todas las variables ya están mapeadas a los registros indicados.

### a. Aritmética combinada {points=5}

```c
res = (a * b) + c;
```

| Variable | Registro |
|----------|----------|
| `a`      | `$s0`    |
| `b`      | `$s1`    |
| `c`      | `$s2`    |
| `res`    | `$t0`    |

```mips title="2a.code"
mult $s0, $s1
mflo $t0
add $t0, $t0, $s2
```

### b. Desplazamiento y acceso a memoria {points=5}

```c
temp = arr[4];
arr[2] = temp + 1;
```

| Variable         | Registro |
|------------------|----------|
| `temp`           | `$t0`    |
| base de `arr`    | `$s0`    |

`arr` es un arreglo de `int` (4 bytes).

```mips title="2b.code"
lw   $t0, 16($s0)
addi $t0, $t0, 1
sw   $t0, 8($s0)
```

### c. Acceso a bytes {points=5}

```c
unsigned char first = str[0];
unsigned char second = str[1];
str[2] = first + second;
```

| Variable         | Registro |
|------------------|----------|
| `first`          | `$t0`    |
| `second`         | `$t1`    |
| base de `str`    | `$s0`    |

`str` es un arreglo de `unsigned char` (1 byte cada elemento).

```mips title="2c.code"
lbu  $t0, 0($s0)
lbu  $t1, 1($s0)
add  $t2, $t0, $t1
sb   $t2, 2($s0)
```

## Ejercicio 3 — Traducción de Función Completa: Suma de Arreglo {points=20}

Traduzca la siguiente función de C a MIPS32.

```c
int sum_array(int arr[], int n) {
    int sum = 0;
    for (int i = 0; i < n; i++) {
        sum = sum + arr[i];
    }
    return sum;
}
```

Asegúrese de utilizar el convenio de llamadas de MIPS32.

```mips title="3.code"
sum_array:
        add  $v0, $zero, $zero
        add  $t0, $zero, $zero
loop:   slt  $t1, $t0, $a1
        beq  $t1, $zero, done
        sll  $t2, $t0, 2
        add  $t2, $a0, $t2
        lw   $t3, 0($t2)
        add  $v0, $v0, $t3
        addi $t0, $t0, 1
        j    loop
done:   jr   $ra
```

## Ejercicio 4 — Traducción con Apuntadores (paso por referencia) {points=20}

Traduzca la siguiente función de C a MIPS32.

```c
void min_max(int values[], int n, int *pmin, int *pmax) {
    *pmin = values[0];
    *pmax = values[0];

    for (int i = 1; i < n; i++) {
        if (values[i] < *pmin) {
            *pmin = values[i];
        }
        if (values[i] > *pmax) {
            *pmax = values[i];
        }
    }
}
```

Asegúrese de utilizar el convenio de llamadas de MIPS32.

```mips title="4.code"
min_max:
        lw   $t0, 0($a0)
        sw   $t0, 0($a2)
        sw   $t0, 0($a3)
        addi $t1, $zero, 1
loop:   slt  $t2, $t1, $a1
        beq  $t2, $zero, done
        sll  $t2, $t1, 2
        add  $t2, $a0, $t2
        lw   $t3, 0($t2)
        lw   $t4, 0($a2)
        slt  $t5, $t3, $t4
        beq  $t5, $zero, checkmax
        sw   $t3, 0($a2)
checkmax: lw $t4, 0($a3)
        slt  $t5, $t4, $t3
        beq  $t5, $zero, next
        sw   $t3, 0($a3)
next:   addi $t1, $t1, 1
        j    loop
done:   jr   $ra
```

## Ejercicio 5 — Traducción Completa: Combinando Arreglos y Apuntadores {points=25}

Traduzca el siguiente programa completo de C a MIPS32.

```c
int multiply(int x, int y) {
    return x * y;
}

void scale_array(int data[], int n, int scale, int *total) {
    int sum = 0;
    for (int i = 0; i < n; i++) {
        data[i] = multiply(data[i], scale);
        sum = sum + data[i];
    }
    *total = sum;
}
```

Asegúrese de utilizar el convenio de llamadas de MIPS32.

```mips title="5.code"
multiply:
        mult $a0, $a1
        mflo $v0
        jr   $ra

scale_array:
        add  $v0, $zero, $zero
        add  $t0, $zero, $zero
loop:   slt  $t1, $t0, $a1
        beq  $t1, $zero, done
        sll  $t2, $t0, 2
        add  $t2, $a0, $t2
        lw   $t3, 0($t2)
        add  $a0, $t3, $zero
        add  $a1, $a3, $zero
        addi $sp, $sp, -4
        sw   $ra, 0($sp)
        jal  multiply
        lw   $ra, 0($sp)
        addi $sp, $sp, 4
        sll  $t2, $t0, 2
        add  $t2, $a0, $t2
        sw   $v0, 0($t2)
        add  $v0, $v0, $v0
        addi $t0, $t0, 1
        j    loop
done:   sw   $v0, 0($a3)
        jr   $ra
```

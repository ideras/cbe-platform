---
title: Examen I Parcial – Organización de Computadoras
duration: 80m
grace: 2m
points: 100
navigation: forward_only
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
| ALU / Escritura de registros    | {{alu}}    |
| Multiplicación                  | {{mult}}   |
| Saltos (branches)               | {{branch}} |
| Saltos incondicionales (jumps)  | {{jump}}   |
| **Total**                       | {{total}}  |

Escriba la traza o el procedimiento que lo llevó a esos números:

```answer {#trace lines=10}
```

### b. Cálculo del CPI {points=10}

Tomando en cuenta la siguiente clasificación de ciclos por instrucción:

- ALU / Escritura de registros → 1 ciclo
- Multiplicación → 2 ciclos
- Saltos (branches) → 2 ciclos
- Saltos incondicionales (jump) → 2 ciclos

Calcule:

1. **(3 pts)** Los ciclos totales del programa: {{cycles}}
2. **(4 pts)** El **CPI** (ciclos por instrucción) del programa: {{cpi}}
3. **(3 pts)** Si se rediseña el multiplicador para que `mult` tarde solo 1 ciclo, ¿cuál sería el nuevo CPI y la **mejora (speedup)** respecto al CPI original?
   Nuevo CPI: {{new_cpi}} — Speedup: {{speedup}}

Muestre sus cálculos:

```answer {#work lines=8}
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

```answer {#code lang=mips}
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

```answer {#code lang=mips}
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

```answer {#code lang=mips}
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

```answer {#code lang=mips}
sum_array:
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

```answer {#code lang=mips}
min_max:
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

```answer {#code lang=mips lines=30}
multiply:

scale_array:
```

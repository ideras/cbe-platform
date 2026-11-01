import { StreamLanguage } from '@codemirror/language';

const instructions = new Set(('add addu addi addiu sub subu mul mult multu div divu rem remu neg negu abs and andi or ori xor xori nor not sll sllv srl srlv sra srav slt sltu slti sltiu seq sne sge sgeu sgt sgtu sle sleu ' +
  'lw sw lb lbu lh lhu sb sh lwl lwr swl swr ll sc lui la li move mfhi mflo mthi mtlo beq bne beqz bnez bgez bgtz blez bltz blt bltu bgt bgtu ble bleu bge bgeu b j jal jalr jr syscall break nop ' +
  'lwc1 swc1 ldc1 sdc1 mtc1 mfc1 bc1t bc1f add.s add.d sub.s sub.d mul.s mul.d div.s div.d mov.s mov.d cvt.s.w cvt.d.w cvt.w.s cvt.w.d c.eq.s c.lt.s c.le.s').split(/\s+/));

export const mipsMode = {
  name: 'mips',
  token(stream) {
    if (stream.eatSpace()) return null;
    if (stream.match('#')) { stream.skipToEnd(); return 'comment'; }
    if (stream.match(/"(?:[^"\\]|\\.)*"?|'(?:[^'\\]|\\.)*'?/)) return 'string';
    if (stream.match(/\$(?:[a-z][a-z0-9]*|\d+)\b/i)) return 'variableName';
    if (stream.match(/(?:0x[\da-f]+|\d+(?:\.\d+)?)(?![\w])/i)) return 'number';
    if (stream.match(/[A-Za-z_][\w.]*\s*:/)) return 'labelName';
    if (stream.match(/\.[a-z][\w.]*/i)) return 'typeName';
    const word = stream.match(/[A-Za-z_][\w.]*/);
    if (word) return instructions.has(word[0].toLowerCase()) ? 'keyword' : null;
    stream.next();
    return null;
  },
  languageData: { commentTokens: { line: '#' } },
};
export const mips = () => StreamLanguage.define(mipsMode);

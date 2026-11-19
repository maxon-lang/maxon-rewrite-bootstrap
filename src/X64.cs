using System.Text;

namespace MiniMaxon;

public enum R { Rax, Rcx, Rdx, Rbx, Rsp, Rbp, Rsi, Rdi, R8, R9, R10, R11, R12, R13, R14, R15 }

public enum Cc { O = 0, NO = 1, B = 2, AE = 3, E = 4, NE = 5, BE = 6, A = 7, S = 8, NS = 9, L = 0xC, GE = 0xD, LE = 0xE, G = 0xF }

public sealed class Label {
  public int Pos = -1;
  public readonly List<int> Uses = [];
  public object? Symbol { get; private init; }

  public static Label Extern(object symbol) => new() { Symbol = symbol };
}

public sealed class CodeUnit {
  public Asm Asm { get; } = new();
  public DataSection Data { get; } = new();
  public Dictionary<object, int> CodeSymbols { get; } = [];
  public Dictionary<object, int> DataSymbols { get; } = [];

  public void DefineCode(object symbol) {
    if (!CodeSymbols.TryAdd(symbol, Asm.Pos)) {
      throw new InternalError($"code symbol {symbol} is defined twice");
    }
  }

  public void DefineData(object symbol, int size) => DataSymbols.Add(symbol, Data.Reserve(size));
}

public sealed class DataSection {
  readonly List<byte> bytes = [];
  readonly Dictionary<string, int> strings = [];

  public byte[] ToArray() => [.. bytes];

  void Align8() {
    while (bytes.Count % 8 != 0) {
      bytes.Add(0);
    }
  }

  public int Reserve(int size) {
    Align8();
    var off = bytes.Count;
    bytes.AddRange(new byte[size]);
    return off;
  }

  public int String(string s) {
    if (strings.TryGetValue(s, out var off)) {
      return off;
    }
    Align8();
    off = bytes.Count;
    var utf8 = Encoding.UTF8.GetBytes(s);
    bytes.AddRange(BitConverter.GetBytes((long)utf8.Length));
    bytes.AddRange(utf8);
    strings[s] = off;
    return off;
  }
}

public sealed class Asm {
  public readonly List<byte> Code = [];
  public readonly List<(int Pos, int DataOffset)> DataFixups = [];
  public readonly List<(int Pos, int Import)> ImportFixups = [];
  public readonly List<(int Pos, object Symbol)> SymbolFixups = [];

  public int Pos => Code.Count;

  public void Raw(params byte[] bs) => Code.AddRange(bs);

  void D32(int v) => Code.AddRange(BitConverter.GetBytes(v));

  void D64(long v) => Code.AddRange(BitConverter.GetBytes(v));

  void Rex(bool w, int reg, int rm, bool force = false) {
    var rex = 0x40 | (w ? 8 : 0) | ((reg >> 3) << 2) | (rm >> 3);
    if (rex != 0x40 || force) {
      Code.Add((byte)rex);
    }
  }

  void ModRR(int reg, int rm) => Code.Add((byte)(0xC0 | ((reg & 7) << 3) | (rm & 7)));

  void Mem(int reg, R b, int disp) {
    var rm = (int)b & 7;
    var r = (reg & 7) << 3;
    var small = disp is >= -128 and <= 127;
    if (disp == 0 && rm != 5) {
      Code.Add((byte)(r | rm));
    } else if (small) {
      Code.Add((byte)(0x40 | r | rm));
    } else {
      Code.Add((byte)(0x80 | r | rm));
    }
    if (rm == 4) {
      Code.Add(0x24);
    }
    if (disp == 0 && rm != 5) {
      return;
    }
    if (small) {
      Code.Add((byte)(sbyte)disp);
    } else {
      D32(disp);
    }
  }

  void RipMem(int reg) => Code.Add((byte)(((reg & 7) << 3) | 5));

  void OpRR(byte op, R dst, R src) {
    Rex(true, (int)src, (int)dst);
    Code.Add(op);
    ModRR((int)src, (int)dst);
  }

  public void Mov(R dst, R src) => OpRR(0x89, dst, src);
  public void Add(R dst, R src) => OpRR(0x01, dst, src);
  public void Sub(R dst, R src) => OpRR(0x29, dst, src);
  public void And(R dst, R src) => OpRR(0x21, dst, src);
  public void Or(R dst, R src) => OpRR(0x09, dst, src);
  public void Xor(R dst, R src) => OpRR(0x31, dst, src);
  public void Cmp(R a, R b) => OpRR(0x39, a, b);
  public void Test(R a, R b) => OpRR(0x85, a, b);

  public void Imul(R dst, R src) {
    Rex(true, (int)dst, (int)src);
    Raw(0x0F, 0xAF);
    ModRR((int)dst, (int)src);
  }

  public void Load(R dst, R b, int disp) {
    Rex(true, (int)dst, (int)b);
    Code.Add(0x8B);
    Mem((int)dst, b, disp);
  }

  public void Store(R b, int disp, R src) {
    Rex(true, (int)src, (int)b);
    Code.Add(0x89);
    Mem((int)src, b, disp);
  }

  public void Lea(R dst, R b, int disp) {
    Rex(true, (int)dst, (int)b);
    Code.Add(0x8D);
    Mem((int)dst, b, disp);
  }

  public void LoadByte(R dst, R b, int disp) {
    Rex(true, (int)dst, (int)b);
    Raw(0x0F, 0xB6);
    Mem((int)dst, b, disp);
  }

  public void MovImm(R dst, long imm) {
    if (imm is >= 0 and <= uint.MaxValue) {
      Rex(false, 0, (int)dst);
      Code.Add((byte)(0xB8 + ((int)dst & 7)));
      D32((int)(uint)imm);
    } else if (imm is >= int.MinValue and <= int.MaxValue) {
      Rex(true, 0, (int)dst);
      Code.Add(0xC7);
      ModRR(0, (int)dst);
      D32((int)imm);
    } else {
      Rex(true, 0, (int)dst);
      Code.Add((byte)(0xB8 + ((int)dst & 7)));
      D64(imm);
    }
  }

  void Group1(int ext, R dst, int imm) {
    Rex(true, 0, (int)dst);
    if (imm is >= -128 and <= 127) {
      Code.Add(0x83);
      ModRR(ext, (int)dst);
      Code.Add((byte)(sbyte)imm);
    } else {
      Code.Add(0x81);
      ModRR(ext, (int)dst);
      D32(imm);
    }
  }

  public void AddImm(R dst, int imm) => Group1(0, dst, imm);
  public void AndImm(R dst, int imm) => Group1(4, dst, imm);
  public void SubImm(R dst, int imm) => Group1(5, dst, imm);
  public void CmpImm(R dst, int imm) => Group1(7, dst, imm);

  void GroupF7(int ext, R r) {
    Rex(true, 0, (int)r);
    Code.Add(0xF7);
    ModRR(ext, (int)r);
  }

  public void Not(R r) => GroupF7(2, r);
  public void Neg(R r) => GroupF7(3, r);
  public void MulRax(R r) => GroupF7(4, r);
  public void Div(R r) => GroupF7(6, r);
  public void Idiv(R r) => GroupF7(7, r);
  public void Cqo() => Raw(0x48, 0x99);

  void ShiftCl(int ext, R r) {
    Rex(true, 0, (int)r);
    Code.Add(0xD3);
    ModRR(ext, (int)r);
  }

  public void ShlCl(R r) => ShiftCl(4, r);
  public void ShrCl(R r) => ShiftCl(5, r);
  public void SarCl(R r) => ShiftCl(7, r);

  public void SarImm(R r, byte n) {
    Rex(true, 0, (int)r);
    Code.Add(0xC1);
    ModRR(7, (int)r);
    Code.Add(n);
  }

  public void Push(R r) {
    Rex(false, 0, (int)r);
    Code.Add((byte)(0x50 + ((int)r & 7)));
  }

  public void Pop(R r) {
    Rex(false, 0, (int)r);
    Code.Add((byte)(0x58 + ((int)r & 7)));
  }

  public void SetccRax(Cc cc) {
    Raw(0x0F, (byte)(0x90 + (int)cc), 0xC0);
    Raw(0x48, 0x0F, 0xB6, 0xC0);
  }

  public void Ret() => Code.Add(0xC3);

  public void RepMovsb() => Raw(0xF3, 0xA4);

  public void LeaData(R dst, int dataOffset) {
    Rex(true, (int)dst, 0);
    Code.Add(0x8D);
    RipMem((int)dst);
    DataFixups.Add((Pos, dataOffset));
    D32(0);
  }

  public void LoadData(R dst, int dataOffset) {
    Rex(true, (int)dst, 0);
    Code.Add(0x8B);
    RipMem((int)dst);
    DataFixups.Add((Pos, dataOffset));
    D32(0);
  }

  public void StoreData(int dataOffset, R src) {
    Rex(true, (int)src, 0);
    Code.Add(0x89);
    RipMem((int)src);
    DataFixups.Add((Pos, dataOffset));
    D32(0);
  }

  public void LoadSymbol(R dst, object symbol) {
    Rex(true, (int)dst, 0);
    Code.Add(0x8B);
    RipMem((int)dst);
    SymbolFixups.Add((Pos, symbol));
    D32(0);
  }

  public void StoreSymbol(object symbol, R src) {
    Rex(true, (int)src, 0);
    Code.Add(0x89);
    RipMem((int)src);
    SymbolFixups.Add((Pos, symbol));
    D32(0);
  }

  public void CallImport(int index) {
    Raw(0xFF, 0x15);
    ImportFixups.Add((Pos, index));
    D32(0);
  }

  void Rel32(Label l) {
    if (l.Symbol is not null) {
      SymbolFixups.Add((Pos, l.Symbol));
      D32(0);
    } else if (l.Pos >= 0) {
      D32(l.Pos - (Pos + 4));
    } else {
      l.Uses.Add(Pos);
      D32(0);
    }
  }

  public void Jmp(Label l) {
    Code.Add(0xE9);
    Rel32(l);
  }

  public void Jcc(Cc cc, Label l) {
    Raw(0x0F, (byte)(0x80 + (int)cc));
    Rel32(l);
  }

  public void Call(Label l) {
    Code.Add(0xE8);
    Rel32(l);
  }

  public void Bind(Label l) {
    if (l.Symbol is not null) {
      throw new InternalError($"external label {l.Symbol} cannot be bound in a unit");
    }
    l.Pos = Pos;
    foreach (var use in l.Uses) {
      var rel = BitConverter.GetBytes(l.Pos - (use + 4));
      for (var i = 0; i < 4; i++) {
        Code[use + i] = rel[i];
      }
    }
    l.Uses.Clear();
  }

  public void Patch32(int pos, int value) {
    var b = BitConverter.GetBytes(value);
    for (var i = 0; i < 4; i++) {
      Code[pos + i] = b[i];
    }
  }

  public int SubRspPlaceholder() {
    Raw(0x48, 0x81, 0xEC);
    var at = Pos;
    D32(0);
    return at;
  }
}

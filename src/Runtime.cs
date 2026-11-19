namespace MiniMaxon;

public static class Rt {
  public const string Entry = "rt.entry";
  public const string Alloc = "rt.alloc";
  public const string Write = "rt.write";
  public const string Panic = "rt.panic";
  public const string Concat = "rt.concat";
  public const string Itoa = "rt.itoa";
  public const string Streq = "rt.streq";
  public const string PanicRange = "rt.panicRange";
  public const string PanicOverflow = "rt.panicOverflow";
  public const string PanicDiv = "rt.panicDiv";
  public const string PanicShift = "rt.panicShift";
  public const string Heap = "g.heap";
  public const string Stdout = "g.stdout";
  public const string Stderr = "g.stderr";

  public static readonly string[] Imports = ["GetProcessHeap", "HeapAlloc", "GetStdHandle", "WriteFile", "ExitProcess"];
  public const string Dll = "kernel32.dll";
}

public sealed class RuntimeUnit {
  const int ImpGetProcessHeap = 0;
  const int ImpHeapAlloc = 1;
  const int ImpGetStdHandle = 2;
  const int ImpWriteFile = 3;
  const int ImpExitProcess = 4;
  const int StdOutputHandle = -11;
  const int StdErrorHandle = -12;
  const int HeapZeroMemory = 8;
  const int PanicExitCode = 1;
  const int Word = 8;

  readonly CodeUnit u = new();
  readonly Label rtAlloc = Label.Extern(Rt.Alloc), rtWrite = Label.Extern(Rt.Write), rtPanic = Label.Extern(Rt.Panic);

  Asm a => u.Asm;
  DataSection data => u.Data;

  public static CodeUnit Emit(FuncDecl main) {
    var g = new RuntimeUnit();
    g.u.DefineData(Rt.Heap, Word);
    g.u.DefineData(Rt.Stdout, Word);
    g.u.DefineData(Rt.Stderr, Word);
    g.EmitEntry(main);
    g.EmitRuntime();
    return g.u;
  }

  void CallRuntime(Label l, int args) {
    a.Call(l);
    if (args > 0) {
      a.AddImm(R.Rsp, args * Word);
    }
  }

  void EmitEntry(FuncDecl main) {
    u.DefineCode(Rt.Entry);
    a.SubImm(R.Rsp, 40);
    a.CallImport(ImpGetProcessHeap);
    a.StoreSymbol(Rt.Heap, R.Rax);
    a.MovImm(R.Rcx, StdOutputHandle);
    a.CallImport(ImpGetStdHandle);
    a.StoreSymbol(Rt.Stdout, R.Rax);
    a.MovImm(R.Rcx, StdErrorHandle);
    a.CallImport(ImpGetStdHandle);
    a.StoreSymbol(Rt.Stderr, R.Rax);
    a.Call(Label.Extern(main));
    a.Mov(R.Rcx, R.Rax);
    a.CallImport(ImpExitProcess);
  }

  void Prologue(int locals, bool callsWindows) {
    a.Push(R.Rbp);
    a.Mov(R.Rbp, R.Rsp);
    if (locals > 0) {
      a.SubImm(R.Rsp, locals);
    }
    if (callsWindows) {
      a.AndImm(R.Rsp, -16);
      a.SubImm(R.Rsp, 48);
    }
  }

  void Epilogue() {
    a.Mov(R.Rsp, R.Rbp);
    a.Pop(R.Rbp);
    a.Ret();
  }

  void EmitRuntime() {
    u.DefineCode(Rt.Alloc);
    Prologue(0, callsWindows: true);
    a.LoadSymbol(R.Rcx, Rt.Heap);
    a.MovImm(R.Rdx, HeapZeroMemory);
    a.Load(R.R8, R.Rbp, 16);
    a.CallImport(ImpHeapAlloc);
    Epilogue();

    u.DefineCode(Rt.Write);
    Prologue(0, callsWindows: true);
    a.Load(R.Rcx, R.Rbp, 24);
    a.Load(R.Rdx, R.Rbp, 16);
    a.Load(R.R8, R.Rdx, 0);
    a.AddImm(R.Rdx, Word);
    a.Lea(R.R9, R.Rsp, 40);
    a.Xor(R.Rax, R.Rax);
    a.Store(R.Rsp, 32, R.Rax);
    a.CallImport(ImpWriteFile);
    Epilogue();

    u.DefineCode(Rt.Panic);
    Prologue(0, callsWindows: false);
    foreach (var piece in new int?[] { data.String("panic: "), null, data.String("\n") }) {
      a.LoadSymbol(R.Rax, Rt.Stderr);
      a.Push(R.Rax);
      if (piece is int off) {
        a.LeaData(R.Rax, off);
      } else {
        a.Load(R.Rax, R.Rbp, 16);
      }
      a.Push(R.Rax);
      CallRuntime(rtWrite, 2);
    }
    a.AndImm(R.Rsp, -16);
    a.SubImm(R.Rsp, 32);
    a.MovImm(R.Rcx, PanicExitCode);
    a.CallImport(ImpExitProcess);

    u.DefineCode(Rt.Concat);
    Prologue(0, callsWindows: false);
    a.Load(R.Rsi, R.Rbp, 24);
    a.Load(R.Rdi, R.Rbp, 16);
    a.Load(R.Rbx, R.Rsi, 0);
    a.Load(R.Rax, R.Rdi, 0);
    a.Add(R.Rbx, R.Rax);
    a.Lea(R.Rax, R.Rbx, Word);
    a.Push(R.Rax);
    CallRuntime(rtAlloc, 1);
    a.Store(R.Rax, 0, R.Rbx);
    a.Mov(R.R12, R.Rax);
    a.Lea(R.Rdi, R.R12, Word);
    foreach (var argOff in new[] { 24, 16 }) {
      a.Load(R.Rsi, R.Rbp, argOff);
      a.Load(R.Rcx, R.Rsi, 0);
      a.AddImm(R.Rsi, Word);
      a.RepMovsb();
    }
    a.Mov(R.Rax, R.R12);
    Epilogue();

    u.DefineCode(Rt.Itoa);
    Prologue(64, callsWindows: false);
    var digits = new Label();
    var loop = new Label();
    var noSign = new Label();
    a.Load(R.Rax, R.Rbp, 24);
    a.Load(R.R8, R.Rbp, 16);
    a.Xor(R.R9, R.R9);
    a.Mov(R.Rsi, R.Rbp);
    a.Test(R.R8, R.R8);
    a.Jcc(Cc.NE, digits);
    a.Test(R.Rax, R.Rax);
    a.Jcc(Cc.NS, digits);
    a.Neg(R.Rax);
    a.MovImm(R.R9, 1);
    a.Bind(digits);
    a.MovImm(R.Rcx, 10);
    a.Bind(loop);
    a.Xor(R.Rdx, R.Rdx);
    a.Div(R.Rcx);
    a.AddImm(R.Rdx, '0');
    a.SubImm(R.Rsi, 1);
    a.Raw(0x88, 0x16);
    a.Test(R.Rax, R.Rax);
    a.Jcc(Cc.NE, loop);
    a.Test(R.R9, R.R9);
    a.Jcc(Cc.E, noSign);
    a.SubImm(R.Rsi, 1);
    a.Raw(0xC6, 0x06, (byte)'-');
    a.Bind(noSign);
    a.Mov(R.Rbx, R.Rbp);
    a.Sub(R.Rbx, R.Rsi);
    a.Lea(R.Rax, R.Rbx, Word);
    a.Push(R.Rax);
    CallRuntime(rtAlloc, 1);
    a.Store(R.Rax, 0, R.Rbx);
    a.Mov(R.R12, R.Rax);
    a.Lea(R.Rdi, R.Rax, Word);
    a.Mov(R.Rcx, R.Rbx);
    a.RepMovsb();
    a.Mov(R.Rax, R.R12);
    Epilogue();

    u.DefineCode(Rt.Streq);
    Prologue(0, callsWindows: false);
    var yes = new Label();
    var no = new Label();
    var done = new Label();
    a.Load(R.Rsi, R.Rbp, 24);
    a.Load(R.Rdi, R.Rbp, 16);
    a.Load(R.Rcx, R.Rsi, 0);
    a.Load(R.Rax, R.Rdi, 0);
    a.Cmp(R.Rcx, R.Rax);
    a.Jcc(Cc.NE, no);
    a.Test(R.Rcx, R.Rcx);
    a.Jcc(Cc.E, yes);
    a.AddImm(R.Rsi, Word);
    a.AddImm(R.Rdi, Word);
    a.Raw(0xF3, 0xA6);
    a.Jcc(Cc.NE, no);
    a.Bind(yes);
    a.MovImm(R.Rax, 1);
    a.Jmp(done);
    a.Bind(no);
    a.MovImm(R.Rax, 0);
    a.Bind(done);
    Epilogue();

    foreach (var (symbol, message) in new[] {
      (Rt.PanicRange, "integer value out of range"),
      (Rt.PanicOverflow, "integer overflow"),
      (Rt.PanicDiv, "division by zero"),
      (Rt.PanicShift, "negative shift count"),
    }) {
      u.DefineCode(symbol);
      a.LeaData(R.Rax, data.String(message));
      a.Push(R.Rax);
      a.Call(rtPanic);
    }
  }
}

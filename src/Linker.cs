namespace MiniMaxon;

public static class Linker {
  const int CodeAlignment = 16;
  const int DataAlignment = 8;

  static int Align(int v, int a) => (v + a - 1) / a * a;

  public static void Link(string outPath, IReadOnlyList<CodeUnit> units, object entry) {
    var code = new List<byte>();
    var data = new List<byte>();
    var codeBase = new int[units.Count];
    var dataBase = new int[units.Count];
    var codeSymbols = new Dictionary<object, int>();
    var dataSymbols = new Dictionary<object, int>();

    for (var i = 0; i < units.Count; i++) {
      var u = units[i];
      codeBase[i] = Align(code.Count, CodeAlignment);
      code.AddRange(new byte[codeBase[i] - code.Count]);
      code.AddRange(u.Asm.Code);
      dataBase[i] = Align(data.Count, DataAlignment);
      data.AddRange(new byte[dataBase[i] - data.Count]);
      data.AddRange(u.Data.ToArray());
      foreach (var (sym, off) in u.CodeSymbols) {
        if (!codeSymbols.TryAdd(sym, codeBase[i] + off)) {
          throw new InternalError($"code symbol {sym} is defined in two units");
        }
      }
      foreach (var (sym, off) in u.DataSymbols) {
        if (!dataSymbols.TryAdd(sym, dataBase[i] + off)) {
          throw new InternalError($"data symbol {sym} is defined in two units");
        }
      }
    }

    var bytes = code.ToArray();
    var entryOffset = codeSymbols.TryGetValue(entry, out var e) ? e : throw new InternalError($"no entry symbol {entry}");

    PeWriter.Write(outPath, bytes, data.ToArray(), Rt.Dll, Rt.Imports, entryOffset, (textRva, dataRva, iatRva) => {
      for (var i = 0; i < units.Count; i++) {
        var a = units[i].Asm;
        foreach (var (pos, off) in a.DataFixups) {
          Patch(bytes, codeBase[i] + pos, textRva, dataRva + (uint)(dataBase[i] + off));
        }
        foreach (var (pos, index) in a.ImportFixups) {
          Patch(bytes, codeBase[i] + pos, textRva, iatRva + (uint)(8 * index));
        }
        foreach (var (pos, sym) in a.SymbolFixups) {
          var target = codeSymbols.TryGetValue(sym, out var c) ? textRva + (uint)c
            : dataSymbols.TryGetValue(sym, out var d) ? dataRva + (uint)d
            : throw new InternalError($"undefined symbol {sym}");
          Patch(bytes, codeBase[i] + pos, textRva, target);
        }
      }
    });
  }

  static void Patch(byte[] code, int pos, uint textRva, uint target) =>
    BitConverter.GetBytes((int)(target - (textRva + (uint)pos + 4))).CopyTo(code, pos);
}

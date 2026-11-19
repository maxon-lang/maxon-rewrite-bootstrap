using System.Collections.Concurrent;

namespace MiniMaxon;

public static class Linker {
  const int CodeAlignment = 16;
  const int DataAlignment = 8;

  static int Align(int v, int a) => (v + a - 1) / a * a;

  static int[] Offsets(IReadOnlyList<int> sizes, int alignment, out int total) {
    var offsets = new int[sizes.Count];
    var at = 0;
    for (var i = 0; i < sizes.Count; i++) {
      offsets[i] = Align(at, alignment);
      at = offsets[i] + sizes[i];
    }
    total = at;
    return offsets;
  }

  public static void Link(string outPath, IReadOnlyList<CodeUnit> units, object entry, ParallelOptions options) {
    var unitData = new byte[units.Count][];
    Parallel.For(0, units.Count, options, i => unitData[i] = units[i].Data.ToArray());

    var codeBase = Offsets([.. units.Select(u => u.Asm.Code.Count)], CodeAlignment, out var codeSize);
    var dataBase = Offsets([.. unitData.Select(d => d.Length)], DataAlignment, out var dataSize);
    var code = new byte[codeSize];
    var data = new byte[dataSize];
    var codeSymbols = new ConcurrentDictionary<object, int>();
    var dataSymbols = new ConcurrentDictionary<object, int>();

    Parallel.For(0, units.Count, options, i => {
      var u = units[i];
      u.Asm.Code.CopyTo(code, codeBase[i]);
      unitData[i].CopyTo(data, dataBase[i]);
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
    });

    var entryOffset = codeSymbols.TryGetValue(entry, out var e) ? e : throw new InternalError($"no entry symbol {entry}");

    PeWriter.Write(outPath, code, data, Rt.Dll, Rt.Imports, entryOffset, (textRva, dataRva, iatRva) => {
      Parallel.For(0, units.Count, options, i => {
        var a = units[i].Asm;
        foreach (var (pos, off) in a.DataFixups) {
          Patch(code, codeBase[i] + pos, textRva, dataRva + (uint)(dataBase[i] + off));
        }
        foreach (var (pos, index) in a.ImportFixups) {
          Patch(code, codeBase[i] + pos, textRva, iatRva + (uint)(8 * index));
        }
        foreach (var (pos, sym) in a.SymbolFixups) {
          var target = codeSymbols.TryGetValue(sym, out var c) ? textRva + (uint)c
            : dataSymbols.TryGetValue(sym, out var d) ? dataRva + (uint)d
            : throw new InternalError($"undefined symbol {sym}");
          Patch(code, codeBase[i] + pos, textRva, target);
        }
      });
    });
  }

  static void Patch(byte[] code, int pos, uint textRva, uint target) =>
    BitConverter.GetBytes((int)(target - (textRva + (uint)pos + 4))).CopyTo(code, pos);
}

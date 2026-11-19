using System.Text;

namespace MiniMaxon;

public static class PeWriter {
  const uint FileAlignment = 0x200;
  const uint SectionAlignment = 0x1000;
  const ulong ImageBase = 0x140000000;
  const uint TextRva = 0x1000;

  static uint AlignUp(uint v, uint a) => (v + a - 1) & ~(a - 1);

  public static void Write(string path, Asm asm, byte[] data, string dll, IReadOnlyList<string> imports, int entryOffset) {
    var code = asm.Code.ToArray();
    var dataRva = TextRva + AlignUp((uint)Math.Max(code.Length, 1), SectionAlignment);
    var idataRva = dataRva + AlignUp((uint)Math.Max(data.Length, 1), SectionAlignment);

    var (idata, iatOffset, iatSize) = BuildImports(idataRva, dll, imports);

    foreach (var (pos, off) in asm.DataFixups) {
      WriteI32(code, pos, (int)(dataRva + off - (TextRva + pos + 4)));
    }
    foreach (var (pos, index) in asm.ImportFixups) {
      WriteI32(code, pos, (int)(idataRva + iatOffset + 8 * index - (TextRva + pos + 4)));
    }

    var sections = new (string Name, byte[] Bytes, uint Rva, uint Flags)[] {
      (".text", code, TextRva, 0x60000020),
      (".data", data.Length == 0 ? new byte[8] : data, dataRva, 0xC0000040),
      (".idata", idata, idataRva, 0xC0000040),
    };

    const uint headerBytes = 64 + 4 + 20 + 240;
    var headersSize = AlignUp(headerBytes + 40 * (uint)sections.Length, FileAlignment);
    var imageSize = idataRva + AlignUp((uint)idata.Length, SectionAlignment);

    using var fs = new FileStream(path, FileMode.Create, FileAccess.Write);
    using var w = new BinaryWriter(fs);

    w.Write((ushort)0x5A4D);
    w.Write(new byte[58]);
    w.Write(64u);

    w.Write(0x00004550u);
    w.Write((ushort)0x8664);
    w.Write((ushort)sections.Length);
    w.Write(0u);
    w.Write(0u);
    w.Write(0u);
    w.Write((ushort)240);
    w.Write((ushort)0x22);

    w.Write((ushort)0x20B);
    w.Write((byte)14);
    w.Write((byte)0);
    w.Write(AlignUp((uint)code.Length, FileAlignment));
    w.Write(AlignUp((uint)data.Length, FileAlignment) + AlignUp((uint)idata.Length, FileAlignment));
    w.Write(0u);
    w.Write(TextRva + (uint)entryOffset);
    w.Write(TextRva);
    w.Write(ImageBase);
    w.Write(SectionAlignment);
    w.Write(FileAlignment);
    w.Write((ushort)6);
    w.Write((ushort)0);
    w.Write((ushort)0);
    w.Write((ushort)0);
    w.Write((ushort)6);
    w.Write((ushort)0);
    w.Write(0u);
    w.Write(imageSize);
    w.Write(headersSize);
    w.Write(0u);
    w.Write((ushort)3);
    w.Write((ushort)0x8160);
    w.Write(0x1000000UL);
    w.Write(0x10000UL);
    w.Write(0x100000UL);
    w.Write(0x1000UL);
    w.Write(0u);
    w.Write(16u);

    for (var i = 0; i < 16; i++) {
      switch (i) {
        case 1:
          w.Write(idataRva);
          w.Write(40u);
          break;
        case 12:
          w.Write(idataRva + iatOffset);
          w.Write(iatSize);
          break;
        default:
          w.Write(0u);
          w.Write(0u);
          break;
      }
    }

    var raw = headersSize;
    foreach (var (name, bytes, rva, flags) in sections) {
      var nameBytes = new byte[8];
      Encoding.ASCII.GetBytes(name).CopyTo(nameBytes, 0);
      w.Write(nameBytes);
      w.Write((uint)bytes.Length);
      w.Write(rva);
      w.Write(AlignUp((uint)bytes.Length, FileAlignment));
      w.Write(raw);
      w.Write(0u);
      w.Write(0u);
      w.Write((ushort)0);
      w.Write((ushort)0);
      w.Write(flags);
      raw += AlignUp((uint)bytes.Length, FileAlignment);
    }

    w.Write(new byte[headersSize - fs.Position]);
    foreach (var (_, bytes, _, _) in sections) {
      w.Write(bytes);
      w.Write(new byte[AlignUp((uint)bytes.Length, FileAlignment) - bytes.Length]);
    }
  }

  static void WriteI32(byte[] buf, int pos, int v) => BitConverter.GetBytes(v).CopyTo(buf, pos);

  static (byte[] Bytes, uint IatOffset, uint IatSize) BuildImports(uint rva, string dll, IReadOnlyList<string> imports) {
    var n = (uint)imports.Count;
    const uint idtSize = 40;
    var iltOffset = idtSize;
    var iatOffset = iltOffset + (n + 1) * 8;
    var namesOffset = iatOffset + (n + 1) * 8;

    var names = new List<byte>();
    var nameRvas = new List<uint>();
    foreach (var name in imports) {
      nameRvas.Add(rva + namesOffset + (uint)names.Count);
      names.AddRange([0, 0]);
      names.AddRange(Encoding.ASCII.GetBytes(name));
      names.Add(0);
      if (names.Count % 2 != 0) {
        names.Add(0);
      }
    }
    var dllRva = rva + namesOffset + (uint)names.Count;
    names.AddRange(Encoding.ASCII.GetBytes(dll));
    names.Add(0);

    using var ms = new MemoryStream();
    using var w = new BinaryWriter(ms);
    w.Write(rva + iltOffset);
    w.Write(0u);
    w.Write(0u);
    w.Write(dllRva);
    w.Write(rva + iatOffset);
    w.Write(new byte[20]);
    for (var table = 0; table < 2; table++) {
      foreach (var nr in nameRvas) {
        w.Write((ulong)nr);
      }
      w.Write(0UL);
    }
    w.Write(names.ToArray());
    return (ms.ToArray(), iatOffset, (n + 1) * 8);
  }
}

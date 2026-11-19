using System.Text;

namespace MiniMaxon;

public sealed record FileSlice(string File, int Begin, int End);

public static class ModuleSource {
  const int MinScratch = 64 * 1024;

  [ThreadStatic]
  static byte[]? scratch;

  public static (char[] Buffer, List<FileSlice> Files) Load(IReadOnlyList<string> files) {
    var total = files.Sum(f => new FileInfo(f).Length);
    var buffer = new char[total];
    var slices = new List<FileSlice>(files.Count);
    var at = 0;

    foreach (var file in files) {
      using var handle = File.OpenHandle(file);
      var length = RandomAccess.GetLength(handle);
      if (length > buffer.Length - at) {
        throw new CompileError(Err.UnexpectedToken, "the file changed while it was being read", file, 1, 1);
      }
      var size = (int)length;
      if (scratch is null || scratch.Length < size) {
        scratch = new byte[Math.Max(size, MinScratch)];
      }
      var read = 0;
      while (read < size) {
        var n = RandomAccess.Read(handle, scratch.AsSpan(read, size - read), read);
        if (n == 0) {
          throw new CompileError(Err.UnexpectedToken, "the file changed while it was being read", file, 1, 1);
        }
        read += n;
      }

      var bytes = scratch.AsSpan(0, size);
      if (bytes.StartsWith("﻿"u8)) {
        bytes = bytes[3..];
      }
      var chars = Encoding.UTF8.GetChars(bytes, buffer.AsSpan(at));
      var end = StripComments(buffer, at, at + chars);
      slices.Add(new FileSlice(file, at, end));
      at = end;
    }
    return (buffer, slices);
  }

  public static int StripComments(char[] b, int begin, int end) {
    var r = begin;
    var w = begin;
    var lineHasCode = false;

    while (r < end) {
      var c = b[r];

      if (c == '\n') {
        b[w++] = c;
        r++;
        lineHasCode = false;
        continue;
      }

      if (c == '/' && r + 1 < end && b[r + 1] == '/') {
        var keep = !lineHasCode && r + 2 < end && b[r + 2] == '/';
        while (r < end && b[r] != '\n') {
          if (keep) {
            b[w++] = b[r];
          }
          r++;
        }
        continue;
      }

      if (c == '/' && r + 1 < end && b[r + 1] == '*') {
        var close = Array.IndexOf(b, '*', r + 2, end - r - 2);
        while (close >= 0 && (close + 1 >= end || b[close + 1] != '/')) {
          close = close + 1 < end ? Array.IndexOf(b, '*', close + 1, end - close - 1) : -1;
        }
        if (close < 0) {
          while (r < end) {
            b[w++] = b[r++];
          }
          break;
        }
        var after = close + 2;
        var lastLineStart = r;
        for (var k = r; k < after; k++) {
          if (b[k] == '\n') {
            b[w++] = '\n';
            lastLineStart = k + 1;
          }
        }
        var codeFollows = after < end && b[after] is not ('\n' or '\r');
        if (codeFollows) {
          for (var k = lastLineStart; k < after; k++) {
            b[w++] = ' ';
          }
        }
        r = after;
        continue;
      }

      if (c == '"') {
        var stop = SkipString(b, r, end);
        while (r < stop) {
          b[w++] = b[r++];
        }
        lineHasCode = true;
        continue;
      }

      if (c == '\'') {
        b[w++] = b[r++];
        while (r < end && b[r] is not ('\'' or '\n')) {
          b[w++] = b[r++];
        }
        if (r < end && b[r] == '\'') {
          b[w++] = b[r++];
        }
        lineHasCode = true;
        continue;
      }

      if (c is not (' ' or '\t' or '\r')) {
        lineHasCode = true;
      }
      b[w++] = c;
      r++;
    }
    return w;
  }

  static int SkipString(char[] b, int open, int end) {
    var i = open + 1;
    while (i < end && b[i] != '\n') {
      switch (b[i]) {
        case '\\':
          i += 2;
          break;
        case '"':
          return i + 1;
        case '{':
          i = SkipInterpolation(b, i + 1, end);
          break;
        default:
          i++;
          break;
      }
    }
    return Math.Min(i, end);
  }

  static int SkipInterpolation(char[] b, int i, int end) {
    var nest = 0;
    while (i < end && b[i] != '\n') {
      switch (b[i]) {
        case '{':
          nest++;
          i++;
          break;
        case '}' when nest == 0:
          return i + 1;
        case '}':
          nest--;
          i++;
          break;
        case '"':
          i++;
          while (i < end && b[i] is not ('"' or '\n')) {
            i += b[i] == '\\' ? 2 : 1;
          }
          i++;
          break;
        default:
          i++;
          break;
      }
    }
    return i;
  }
}

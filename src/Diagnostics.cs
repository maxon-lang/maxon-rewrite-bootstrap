namespace MiniMaxon;

public sealed class CompileError(string code, string message, string file, int line, int col) : Exception(message) {
  public string Code { get; } = code;
  public string File { get; } = file;
  public int Line { get; } = line;
  public int Col { get; } = col;

  public override string ToString() => $"{File}:{Line}:{Col}: error {Code}: {Message}";
}

public sealed class InternalError(string message) : Exception(message);

public static class Err {
  public const string UnexpectedToken = "E2001";
  public const string ExpectedType = "E2003";
  public const string ExpressionError = "E2004";
  public const string MismatchedEndLabel = "E2008";
  public const string IntegerLiteralRange = "E2011";
  public const string ExpectedToken = "E2010";
  public const string AssignToImmutable = "E2013";
  public const string MatchNotExhaustive = "E2026";
  public const string MatchDefaultEnumMustThrow = "E2046";
  public const string MatchBlockStatement = "E2049";
  public const string CallFirstArgNamed = "E2052";
  public const string CallArgMissingLabel = "E2053";
  public const string NoMainFunction = "E3001";
  public const string MainWrongReturnType = "E3002";
  public const string UnknownFunction = "E3004";
  public const string TypeMismatch = "E3005";
  public const string DuplicateDefinition = "E3006";
  public const string UnknownType = "E3011";
  public const string MissingReturn = "E3013";
  public const string UnknownField = "E3018";
  public const string EnumDuplicateCase = "E3030";
  public const string EnumUnknownCase = "E3034";
  public const string EnumWrongBindingCount = "E3035";
  public const string CallArgCountMismatch = "E3036";
  public const string CallUnknownArgLabel = "E3037";
  public const string CallDuplicateArg = "E3038";
  public const string EnumNotComparable = "E3066";
  public const string ConstructorRestriction = "E3076";
  public const string EmptyBlock = "E3082";
  public const string FieldNotInitialized = "E3086";
  public const string UseAfterMove = "E3102";
  public const string DivisionByZero = "E3103";
  public const string UnsignedNegation = "E3218";
  public const string TypeCycle = "E4014";
  public const string Unsupported = "E9000";
}

namespace Gaffer.Runtime.Errors;

internal static class ErrorFormatter {
	public static string FormatInvalidProjection(string description, string source, int? line, int? column) {
		if (line == null || column == null)
			return $"Invalid projection definition\n\nerror: {description}\n";

		return $"Failed to compile projection\n\nerror: {description}\n" +
			FormatSnippet(source, description, line.Value, column.Value);
	}

	public static string FormatHandlerError(
		string description, string source, string eventType, string streamId,
		long sequenceNumber, string? partition, string? jsStack, int? line, int? column) {
		var result = $"Error in '{eventType}' handler\n\nHandler threw: {description}\n";
		if (line != null && column != null)
			result += FormatSnippet(source, description, line.Value, column.Value);
		if (jsStack != null)
			result += FormatJsStack(jsStack);
		result += "\n";
		result += FormatEventContext(eventType, streamId, sequenceNumber, partition);
		return result;
	}

	public static string FormatTransformError(
		string description, string source, string? jsStack, int? line, int? column) {
		var result = $"Transform error\n\nerror: {description}\n";
		if (line != null && column != null)
			result += FormatSnippet(source, description, line.Value, column.Value);
		if (jsStack != null)
			result += FormatJsStack(jsStack);
		return result;
	}

	private static string FormatSnippet(string source, string description, int line, int column) {
		var lines = source.Split('\n');
		if (line < 1 || line > lines.Length)
			return "";

		var errorLine = lines[line - 1];
		var startContext = Math.Max(0, line - 3);
		var gutter = line.ToString().Length + 1;

		var allLines = new List<string>();
		for (var i = startContext; i < line - 1 && i < lines.Length; i++)
			allLines.Add(lines[i]);
		allLines.Add(errorLine);

		var minIndent = int.MaxValue;
		for (var i = 0; i < allLines.Count; i++) {
			var l = allLines[i];
			if (string.IsNullOrWhiteSpace(l))
				continue;
			var indent = 0;
			for (var j = 0; j < l.Length; j++) {
				if (l[j] == ' ' || l[j] == '\t')
					indent++;
				else
					break;
			}
			if (indent < minIndent)
				minIndent = indent;
		}
		if (minIndent == int.MaxValue)
			minIndent = 0;

		var adjustedColumn = Math.Max(0, column - minIndent);
		var pad = new string(' ', gutter);
		var result = $" {pad} ┌─ {line}:{adjustedColumn + 1}\n";
		result += $" {pad} │\n";

		for (var i = startContext; i < line - 1 && i < lines.Length; i++) {
			var num = (i + 1).ToString().PadLeft(gutter);
			var stripped = lines[i].Length > minIndent ? lines[i][minIndent..] : "";
			result += $" {num} │ {stripped}\n";
		}

		var strippedError = errorLine.Length > minIndent ? errorLine[minIndent..] : errorLine;
		result += $" {line.ToString().PadLeft(gutter)} │ {strippedError}\n";

		var caretPad = "";
		if (adjustedColumn > 0 && adjustedColumn <= strippedError.Length) {
			var chars = strippedError[..adjustedColumn].ToCharArray();
			for (var i = 0; i < chars.Length; i++)
				if (chars[i] != '\t')
					chars[i] = ' ';
			caretPad = new string(chars);
		} else if (adjustedColumn > 0) {
			caretPad = new string(' ', adjustedColumn);
		}

		result += $" {pad} │ {caretPad}^ {description}\n";
		result += $" {pad} │\n";

		return result;
	}

	private static string FormatJsStack(string jsStack) {
		var result = "";
		foreach (var line in jsStack.Split('\n'))
			result += $"  {line.Trim()}\n";
		return result;
	}

	public static string FormatWithEventContext(
		string description, string eventType, string streamId, long sequenceNumber, string? partition) {
		return $"{description}\n\n" + FormatEventContext(eventType, streamId, sequenceNumber, partition);
	}

	public static string FormatStateSerializationError(
		string description, string eventType, string streamId, long sequenceNumber, string? partition) {
		return FormatWithEventContext($"Failed to serialize projection state: {description}", eventType, streamId, sequenceNumber, partition);
	}

	private static string FormatEventContext(string eventType, string streamId, long sequenceNumber, string? partition) {
		var result = $"Event: {sequenceNumber}@{streamId}\n";
		result += $"Type:  {eventType}\n";
		if (partition != null)
			result += $"Partition: {partition}\n";
		return result;
	}
}

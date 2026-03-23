namespace Gaffer.Runtime.Errors;

internal sealed class MalformedEventDataException : Exception {
	public string Field { get; }

	public MalformedEventDataException(string field, Exception innerException)
		: base($"Event {field} is not valid JSON", innerException) {
		Field = field;
	}
}

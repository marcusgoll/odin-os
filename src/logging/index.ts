export type LogLevel = "debug" | "info" | "warn" | "error";

export interface LogRecord {
  readonly level: LogLevel;
  readonly message: string;
  readonly fields?: Record<string, unknown>;
}

export interface StructuredLogger {
  log(record: LogRecord): void;
  info(message: string, fields?: Record<string, unknown>): void;
  warn(message: string, fields?: Record<string, unknown>): void;
  error(message: string, fields?: Record<string, unknown>): void;
}

export function createConsoleLogger(options: { readonly level: LogLevel }): StructuredLogger {
  const record = (level: LogLevel, message: string, fields?: Record<string, unknown>): LogRecord => {
    if (fields === undefined) {
      return { level, message };
    }

    return { level, message, fields };
  };

  const write = (record: LogRecord): void => {
    const payload = {
      timestamp: new Date().toISOString(),
      level: record.level,
      message: record.message,
      fields: record.fields ?? {}
    };

    if (record.level === "error") {
      console.error(JSON.stringify(payload));
      return;
    }

    console.log(JSON.stringify(payload));
  };

  return {
    log: write,
    info: (message, fields) => write(record(options.level === "debug" ? "debug" : "info", message, fields)),
    warn: (message, fields) => write(record("warn", message, fields)),
    error: (message, fields) => write(record("error", message, fields))
  };
}

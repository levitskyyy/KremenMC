package kremenmc.bridge;

import java.io.PrintStream;
import java.io.PrintWriter;
import java.io.StringWriter;
import java.nio.charset.StandardCharsets;
import java.util.logging.Handler;
import java.util.logging.Level;
import java.util.logging.LogManager;
import java.util.logging.LogRecord;
import java.util.logging.Logger;

/** Formats java.util.logging records as "LEVEL|logger|message" lines that the Go supervisor parses. */
final class LogSetup {
    private LogSetup() {}

    private static final PrintStream OUT = new PrintStream(new java.io.FileOutputStream(java.io.FileDescriptor.out), true, StandardCharsets.UTF_8);

    static void install() {
        LogManager.getLogManager().reset();
        Logger root = Logger.getLogger("");
        root.setLevel(Level.INFO);
        root.addHandler(new Handler() {
            @Override
            public void publish(LogRecord r) {
                if (!isLoggable(r)) return;
                String name = r.getLoggerName() == null ? "" : r.getLoggerName();
                String msg = r.getMessage();
                if (msg != null && r.getParameters() != null && r.getParameters().length > 0) {
                    try {
                        msg = java.text.MessageFormat.format(msg, r.getParameters());
                    } catch (IllegalArgumentException ignored) {
                    }
                }
                StringBuilder sb = new StringBuilder();
                String prefix = r.getLevel().getName() + "|" + name.replace('|', '/') + "|";
                append(sb, prefix, msg == null ? "" : msg);
                if (r.getThrown() != null) {
                    StringWriter sw = new StringWriter();
                    r.getThrown().printStackTrace(new PrintWriter(sw));
                    append(sb, prefix, sw.toString());
                }
                synchronized (OUT) {
                    OUT.print(sb);
                    OUT.flush();
                }
            }

            private void append(StringBuilder sb, String prefix, String text) {
                for (String line : text.split("\r?\n")) sb.append(prefix).append(line).append('\n');
            }

            @Override
            public void flush() { OUT.flush(); }

            @Override
            public void close() {}
        });
    }
}

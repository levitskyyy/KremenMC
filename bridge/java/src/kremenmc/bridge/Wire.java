package kremenmc.bridge;

import java.io.ByteArrayOutputStream;
import java.io.DataInputStream;
import java.io.IOException;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collection;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.UUID;

/** Mirror of internal/bridge/protocol.go. */
final class Wire {
    private Wire() {}

    static final int PROTOCOL_VERSION = 1;
    static final int MAX_FRAME = 16 << 20;

    static final byte KIND_CALL = 1, KIND_REPLY = 2, KIND_ERROR = 3, KIND_NOTIFY = 4;
    static final byte TAG_NIL = 0, TAG_FALSE = 1, TAG_TRUE = 2, TAG_INT = 3, TAG_FLOAT = 4,
            TAG_STRING = 5, TAG_BYTES = 6, TAG_LIST = 7, TAG_MAP = 8, TAG_UUID = 9;

    static final int OP_HELLO = 1, OP_LISTENERS = 2, OP_PLUGIN_STATE = 3,
            OP_PLAYERS = 10, OP_PLAYER = 11, OP_SEND_MESSAGE = 12, OP_BROADCAST = 13, OP_KICK = 14,
            OP_TELEPORT = 15, OP_HAS_PERMISSION = 16,
            OP_WORLDS = 20, OP_GET_BLOCK = 21, OP_SET_BLOCK = 22,
            OP_REGISTER_COMMAND = 30, OP_DISPATCH_COMMAND = 31;

    static final int OP_LOAD_PLUGINS = 100, OP_EVENT = 101, OP_TICK = 102, OP_COMMAND = 103,
            OP_TAB_COMPLETE = 104, OP_SHUTDOWN = 105;

    record Frame(byte kind, int id, int op, List<Object> args) {}

    static byte[] encode(byte kind, int id, int op, Object... args) {
        Out o = new Out();
        o.raw(new byte[4]);
        o.b(kind);
        o.i32(id);
        o.b((byte) (op >>> 8));
        o.b((byte) op);
        for (Object a : args) o.value(a, 0);
        byte[] buf = o.toByteArray();
        int n = buf.length - 4;
        if (n > MAX_FRAME) throw new IllegalArgumentException("frame too large: " + n);
        ByteBuffer.wrap(buf).putInt(0, n);
        return buf;
    }

    static Frame read(DataInputStream in) throws IOException {
        int n = in.readInt();
        if (n < 7 || n > MAX_FRAME) throw new IOException("bad frame length " + n);
        byte[] buf = new byte[n];
        in.readFully(buf);
        ByteBuffer bb = ByteBuffer.wrap(buf);
        byte kind = bb.get();
        int id = bb.getInt();
        int op = bb.getShort() & 0xffff;
        List<Object> args = new ArrayList<>();
        while (bb.hasRemaining()) args.add(value(bb, 0));
        return new Frame(kind, id, op, args);
    }

    private static final class Out extends ByteArrayOutputStream {
        Out() { super(256); }

        void b(byte v) { write(v); }

        void raw(byte[] v) { write(v, 0, v.length); }

        void i32(int v) {
            write(v >>> 24); write(v >>> 16); write(v >>> 8); write(v);
        }

        void uvarint(long v) {
            while ((v & ~0x7FL) != 0) {
                write((int) ((v & 0x7F) | 0x80));
                v >>>= 7;
            }
            write((int) v);
        }

        void str(String s) {
            byte[] b = s.getBytes(StandardCharsets.UTF_8);
            uvarint(b.length);
            raw(b);
        }

        void value(Object v, int depth) {
            if (depth > 64) throw new IllegalArgumentException("value nesting too deep");
            switch (v) {
                case null -> b(TAG_NIL);
                case Boolean x -> b(x ? TAG_TRUE : TAG_FALSE);
                case Long x -> integer(x);
                case Integer x -> integer(x);
                case Short x -> integer(x);
                case Byte x -> integer(x);
                case Double x -> real(x);
                case Float x -> real(x);
                case String x -> { b(TAG_STRING); str(x); }
                case byte[] x -> { b(TAG_BYTES); uvarint(x.length); raw(x); }
                case UUID x -> {
                    b(TAG_UUID);
                    long hi = x.getMostSignificantBits(), lo = x.getLeastSignificantBits();
                    for (int i = 56; i >= 0; i -= 8) write((int) (hi >>> i));
                    for (int i = 56; i >= 0; i -= 8) write((int) (lo >>> i));
                }
                case Collection<?> x -> {
                    b(TAG_LIST);
                    uvarint(x.size());
                    for (Object e : x) value(e, depth + 1);
                }
                case Object[] x -> {
                    b(TAG_LIST);
                    uvarint(x.length);
                    for (Object e : x) value(e, depth + 1);
                }
                case Map<?, ?> x -> {
                    b(TAG_MAP);
                    uvarint(x.size());
                    for (Map.Entry<?, ?> e : x.entrySet()) {
                        str(String.valueOf(e.getKey()));
                        value(e.getValue(), depth + 1);
                    }
                }
                case Enum<?> x -> { b(TAG_STRING); str(x.name()); }
                default -> throw new IllegalArgumentException("unsupported wire type " + v.getClass().getName());
            }
        }

        void integer(long v) {
            b(TAG_INT);
            uvarint((v << 1) ^ (v >> 63));
        }

        void real(double v) {
            b(TAG_FLOAT);
            long bits = Double.doubleToRawLongBits(v);
            for (int i = 56; i >= 0; i -= 8) write((int) (bits >>> i));
        }
    }

    private static long uvarint(ByteBuffer bb) throws IOException {
        long x = 0;
        for (int s = 0; s < 70; s += 7) {
            if (!bb.hasRemaining()) throw new IOException("truncated varint");
            int b = bb.get() & 0xff;
            x |= (long) (b & 0x7f) << s;
            if (b < 0x80) return x;
        }
        throw new IOException("varint overflow");
    }

    private static byte[] bytes(ByteBuffer bb) throws IOException {
        long n = uvarint(bb);
        if (n < 0 || n > bb.remaining()) throw new IOException("truncated value");
        byte[] b = new byte[(int) n];
        bb.get(b);
        return b;
    }

    private static Object value(ByteBuffer bb, int depth) throws IOException {
        if (depth > 64) throw new IOException("value nesting too deep");
        if (!bb.hasRemaining()) throw new IOException("truncated value");
        byte tag = bb.get();
        switch (tag) {
            case TAG_NIL: return null;
            case TAG_FALSE: return Boolean.FALSE;
            case TAG_TRUE: return Boolean.TRUE;
            case TAG_INT: {
                long u = uvarint(bb);
                return (u >>> 1) ^ -(u & 1);
            }
            case TAG_FLOAT:
                if (bb.remaining() < 8) throw new IOException("truncated float");
                return bb.getDouble();
            case TAG_STRING: return new String(bytes(bb), StandardCharsets.UTF_8);
            case TAG_BYTES: return bytes(bb);
            case TAG_UUID:
                if (bb.remaining() < 16) throw new IOException("truncated uuid");
                return new UUID(bb.getLong(), bb.getLong());
            case TAG_LIST: {
                long n = uvarint(bb);
                if (n > bb.remaining()) throw new IOException("truncated list");
                List<Object> l = new ArrayList<>((int) n);
                for (long i = 0; i < n; i++) l.add(value(bb, depth + 1));
                return l;
            }
            case TAG_MAP: {
                long n = uvarint(bb);
                if (n > bb.remaining()) throw new IOException("truncated map");
                Map<String, Object> m = new LinkedHashMap<>();
                for (long i = 0; i < n; i++) {
                    String k = new String(bytes(bb), StandardCharsets.UTF_8);
                    m.put(k, value(bb, depth + 1));
                }
                return m;
            }
            default: throw new IOException("unknown tag " + tag);
        }
    }

    static String str(List<Object> a, int i) {
        return i < a.size() && a.get(i) instanceof String s ? s : "";
    }

    static long num(List<Object> a, int i) {
        if (i >= a.size()) return 0;
        return a.get(i) instanceof Number n ? n.longValue() : 0;
    }

    static boolean bool(List<Object> a, int i) {
        return i < a.size() && a.get(i) instanceof Boolean b && b;
    }

    static UUID uuid(List<Object> a, int i) {
        return i < a.size() && a.get(i) instanceof UUID u ? u : null;
    }

    @SuppressWarnings("unchecked")
    static Map<String, Object> map(Object o) {
        return o instanceof Map<?, ?> m ? (Map<String, Object>) m : Map.of();
    }

    @SuppressWarnings("unchecked")
    static List<Object> list(Object o) {
        return o instanceof List<?> l ? (List<Object>) l : List.of();
    }

    static String str(Map<String, Object> m, String k) {
        return m.get(k) instanceof String s ? s : "";
    }

    static double dbl(Map<String, Object> m, String k) {
        return m.get(k) instanceof Number n ? n.doubleValue() : 0;
    }

    static long lng(Map<String, Object> m, String k) {
        return m.get(k) instanceof Number n ? n.longValue() : 0;
    }

    static boolean bool(Map<String, Object> m, String k) {
        return m.get(k) instanceof Boolean b && b;
    }
}

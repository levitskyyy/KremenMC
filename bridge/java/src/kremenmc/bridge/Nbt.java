package kremenmc.bridge;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.io.IOException;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Network NBT (nameless root) for custom_data. Compounds are
 * LinkedHashMap<String, Object>, lists are TagList, primitives use their
 * boxed Java type, arrays byte[] / int[] / long[].
 */
final class Nbt {
    private Nbt() {}

    static final byte END = 0, BYTE = 1, SHORT = 2, INT = 3, LONG = 4, FLOAT = 5, DOUBLE = 6,
            BYTE_ARRAY = 7, STRING = 8, LIST = 9, COMPOUND = 10, INT_ARRAY = 11, LONG_ARRAY = 12;

    /** An NBT list keeps its element type, which an empty list cannot infer. */
    static final class TagList extends ArrayList<Object> {
        byte type;
        TagList(byte type) { this.type = type; }
    }

    static Map<String, Object> read(byte[] b) {
        if (b == null || b.length == 0) return new LinkedHashMap<>();
        try {
            DataInputStream in = new DataInputStream(new ByteArrayInputStream(b));
            byte t = in.readByte();
            if (t != COMPOUND) return new LinkedHashMap<>();
            return compound(in, 0);
        } catch (IOException e) {
            return new LinkedHashMap<>();
        }
    }

    static byte[] write(Map<String, Object> c) {
        try {
            ByteArrayOutputStream bo = new ByteArrayOutputStream();
            DataOutputStream out = new DataOutputStream(bo);
            out.writeByte(COMPOUND);
            compound(out, c);
            return bo.toByteArray();
        } catch (IOException e) {
            throw new IllegalStateException(e);
        }
    }

    private static Map<String, Object> compound(DataInputStream in, int depth) throws IOException {
        Map<String, Object> m = new LinkedHashMap<>();
        for (;;) {
            byte t = in.readByte();
            if (t == END) return m;
            String k = in.readUTF();
            m.put(k, payload(in, t, depth + 1));
        }
    }

    private static Object payload(DataInputStream in, byte t, int depth) throws IOException {
        if (depth > 512) throw new IOException("nbt too deep");
        switch (t) {
            case BYTE: return in.readByte();
            case SHORT: return in.readShort();
            case INT: return in.readInt();
            case LONG: return in.readLong();
            case FLOAT: return in.readFloat();
            case DOUBLE: return in.readDouble();
            case BYTE_ARRAY: { byte[] a = new byte[len(in)]; in.readFully(a); return a; }
            case STRING: return in.readUTF();
            case LIST: {
                byte et = in.readByte();
                int n = len(in);
                TagList l = new TagList(et);
                for (int i = 0; i < n; i++) l.add(payload(in, et, depth + 1));
                return l;
            }
            case COMPOUND: return compound(in, depth);
            case INT_ARRAY: { int n = len(in); int[] a = new int[n]; for (int i = 0; i < n; i++) a[i] = in.readInt(); return a; }
            case LONG_ARRAY: { int n = len(in); long[] a = new long[n]; for (int i = 0; i < n; i++) a[i] = in.readLong(); return a; }
            default: throw new IOException("bad nbt tag " + t);
        }
    }

    private static int len(DataInputStream in) throws IOException {
        int n = in.readInt();
        if (n < 0 || n > (1 << 24)) throw new IOException("bad nbt length");
        return n;
    }

    static byte tagOf(Object v) {
        return switch (v) {
            case Byte x -> BYTE;
            case Boolean x -> BYTE;
            case Short x -> SHORT;
            case Integer x -> INT;
            case Long x -> LONG;
            case Float x -> FLOAT;
            case Double x -> DOUBLE;
            case byte[] x -> BYTE_ARRAY;
            case String x -> STRING;
            case List<?> x -> LIST;
            case Map<?, ?> x -> COMPOUND;
            case int[] x -> INT_ARRAY;
            case long[] x -> LONG_ARRAY;
            default -> throw new IllegalArgumentException("not an nbt value: " + v.getClass().getName());
        };
    }

    @SuppressWarnings("unchecked")
    private static void compound(DataOutputStream out, Map<String, Object> c) throws IOException {
        for (Map.Entry<String, Object> e : c.entrySet()) {
            byte t = tagOf(e.getValue());
            out.writeByte(t);
            out.writeUTF(e.getKey());
            payload(out, t, e.getValue());
        }
        out.writeByte(END);
    }

    @SuppressWarnings("unchecked")
    private static void payload(DataOutputStream out, byte t, Object v) throws IOException {
        switch (t) {
            case BYTE -> out.writeByte(v instanceof Boolean b ? (b ? 1 : 0) : (Byte) v);
            case SHORT -> out.writeShort((Short) v);
            case INT -> out.writeInt((Integer) v);
            case LONG -> out.writeLong((Long) v);
            case FLOAT -> out.writeFloat((Float) v);
            case DOUBLE -> out.writeDouble((Double) v);
            case BYTE_ARRAY -> { byte[] a = (byte[]) v; out.writeInt(a.length); out.write(a); }
            case STRING -> out.writeUTF((String) v);
            case LIST -> {
                List<Object> l = (List<Object>) v;
                byte et = l instanceof TagList tl ? tl.type : l.isEmpty() ? END : tagOf(l.getFirst());
                out.writeByte(et);
                out.writeInt(l.size());
                for (Object o : l) payload(out, et, o);
            }
            case COMPOUND -> compound(out, (Map<String, Object>) v);
            case INT_ARRAY -> { int[] a = (int[]) v; out.writeInt(a.length); for (int x : a) out.writeInt(x); }
            case LONG_ARRAY -> { long[] a = (long[]) v; out.writeInt(a.length); for (long x : a) out.writeLong(x); }
            default -> throw new IOException("bad nbt tag " + t);
        }
    }

    @SuppressWarnings("unchecked")
    static Object deepCopy(Object v) {
        return switch (v) {
            case TagList l -> { TagList c = new TagList(l.type); for (Object o : l) c.add(deepCopy(o)); yield c; }
            case Map<?, ?> m -> { Map<String, Object> c = new LinkedHashMap<>(); ((Map<String, Object>) m).forEach((k, x) -> c.put(k, deepCopy(x))); yield c; }
            case byte[] a -> a.clone();
            case int[] a -> a.clone();
            case long[] a -> a.clone();
            default -> v;
        };
    }
}

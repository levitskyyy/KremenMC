package kremenmc.probe.item;

import com.google.common.hash.HashCode;
import java.nio.ByteBuffer;
import java.util.*;
import java.util.stream.*;
import com.mojang.datafixers.util.Pair;
import net.minecraft.util.HashOps;

public final class HashPrims {
    static void p(String name, HashCode h) { System.out.println(name + " " + h.asInt() + " " + h); }
    public static void main(String[] args) {
        HashOps o = HashOps.CRC32C_INSTANCE;
        p("empty", o.empty());
        p("emptyMap", o.emptyMap());
        p("emptyList", o.emptyList());
        p("true", o.createBoolean(true));
        p("false", o.createBoolean(false));
        p("byte1", o.createByte((byte) 1));
        p("short1", o.createShort((short) 1));
        p("int1", o.createInt(1));
        p("int-2", o.createInt(-2));
        p("long1", o.createLong(1));
        p("float1", o.createFloat(1f));
        p("double1", o.createDouble(1d));
        p("str_", o.createString(""));
        p("strab", o.createString("ab"));
        p("strU", o.createString("é😀"));
        p("list12", o.createList(Stream.of(o.createInt(1), o.createInt(2))));
        p("list21", o.createList(Stream.of(o.createInt(2), o.createInt(1))));
        p("map_a1", o.createMap(Stream.of(Pair.of(o.createString("a"), o.createInt(1)))));
        p("map_a1b2", o.createMap(Stream.of(Pair.of(o.createString("a"), o.createInt(1)), Pair.of(o.createString("b"), o.createInt(2)))));
        p("map_b2a1", o.createMap(Stream.of(Pair.of(o.createString("b"), o.createInt(2)), Pair.of(o.createString("a"), o.createInt(1)))));
        p("bytes123", o.createByteList(ByteBuffer.wrap(new byte[]{1, 2, 3})));
        p("ints12", o.createIntList(IntStream.of(1, 2)));
        p("longs12", o.createLongList(LongStream.of(1, 2)));
        System.out.println("EMPTY_MAP_PAYLOAD " + Arrays.toString(HashOps.EMPTY_MAP_PAYLOAD));
        System.out.println("EMPTY_LIST_PAYLOAD " + Arrays.toString(HashOps.EMPTY_LIST_PAYLOAD));
    }
}

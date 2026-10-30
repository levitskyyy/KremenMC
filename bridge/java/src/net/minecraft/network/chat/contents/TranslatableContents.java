package net.minecraft.network.chat.contents;

import java.util.Arrays;
import java.util.Objects;
import net.minecraft.network.chat.ComponentContents;

public final class TranslatableContents implements ComponentContents {
    public static final Object[] NO_ARGS = new Object[0];

    private final String key;
    private final String fallback;
    private final Object[] args;

    public TranslatableContents(String key, String fallback, Object[] args) {
        this.key = key;
        this.fallback = fallback;
        this.args = args == null ? NO_ARGS : args;
    }

    public String getKey() { return key; }
    public String getFallback() { return fallback; }
    public Object[] getArgs() { return args; }

    @Override
    public boolean equals(Object o) {
        return o instanceof TranslatableContents t && key.equals(t.key) && Objects.equals(fallback, t.fallback) && Arrays.equals(args, t.args);
    }

    @Override public int hashCode() { return Objects.hash(key, fallback, Arrays.hashCode(args)); }
    @Override public String toString() { return "translation{key='" + key + "', args=" + Arrays.toString(args) + "}"; }
}

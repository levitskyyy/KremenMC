package net.minecraft.network;

import io.netty.buffer.ByteBuf;

/** Holds the wrapped buffer; the bridge does not encode game data on the Java side. */
public class FriendlyByteBuf {
    private final ByteBuf source;

    public FriendlyByteBuf(ByteBuf source) { this.source = source; }

    public ByteBuf unwrap() { return source; }
}

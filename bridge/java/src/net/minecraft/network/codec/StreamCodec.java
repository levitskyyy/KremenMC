package net.minecraft.network.codec;

public interface StreamCodec<B, V> extends StreamDecoder<B, V>, StreamEncoder<B, V> {
    static <B, V> StreamCodec<B, V> of(StreamEncoder<B, V> encoder, StreamDecoder<B, V> decoder) {
        return new StreamCodec<>() {
            @Override public V decode(B buffer) { return decoder.decode(buffer); }
            @Override public void encode(B buffer, V value) { encoder.encode(buffer, value); }
        };
    }
}

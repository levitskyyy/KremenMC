package net.minecraft.world.item;

import net.minecraft.network.RegistryFriendlyByteBuf;
import net.minecraft.network.codec.StreamCodec;
import net.minecraft.world.level.ItemLike;

/** Placeholder: item data lives on the Go side and is not encoded here. */
public final class ItemStack {
    public static final ItemStack EMPTY = new ItemStack(null, 0);
    public static final StreamCodec<RegistryFriendlyByteBuf, ItemStack> OPTIONAL_STREAM_CODEC = StreamCodec.of(
            (buf, stack) -> { throw new UnsupportedOperationException("kremenmc bridge: NMS item stack encoding is not supported"); },
            buf -> { throw new UnsupportedOperationException("kremenmc bridge: NMS item stack decoding is not supported"); });

    private final Item item;
    private int count;

    public ItemStack(ItemLike item, int count) {
        this.item = item == null ? null : item.asItem();
        this.count = count;
    }

    public Item getItem() { return item; }
    public int getCount() { return count; }
    public void setCount(int count) { this.count = count; }
    public boolean isEmpty() { return item == null || count <= 0; }
}

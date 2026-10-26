import 'dart:async';
import 'package:flutter/material.dart';
import 'go_commands.dart';

/// PakarController's deliberate, peer-bound probe adapted to a linear strip.
/// These bytes are requested output, not optical or board RGB readback.
List<int> pixelProbe(int count,int index,List<int> rgb) {
  if(count<1||count>100||index<0||index>=count||rgb.length!=3||rgb.any((v)=>v<0||v>255))throw ArgumentError('Invalid pixel probe');
  final bytes=List<int>.filled(count*3,0);bytes.setRange(index*3,index*3+3,rgb);return bytes;
}

class PixelDiagnostic extends StatefulWidget {
  const PixelDiagnostic({super.key,required this.client,required this.count});
  final GoCommandClient client;final int count;
  @override State<PixelDiagnostic> createState()=>_PixelDiagnosticState();
}
class _PixelDiagnosticState extends State<PixelDiagnostic> {
  int index=0;String? armedOwner,receipt;bool pending=false;Timer? expiration;
  bool get ready=>widget.client.current&&widget.client.session.boardReady&&!widget.client.busy&&!pending;
  void disarm(){expiration?.cancel();armedOwner=null;}
  @override void dispose(){expiration?.cancel();super.dispose();}
  @override void didUpdateWidget(covariant PixelDiagnostic old){super.didUpdateWidget(old);if(old.count!=widget.count||old.client!=widget.client){disarm();index=index.clamp(0,widget.count-1);receipt=null;}}
  Future<void> send(List<int> rgb,{bool off=false})async {
    if(!ready||!off&&armedOwner!=widget.client.session.targetStamp)return;
    disarm();setState((){pending=true;receipt=null;});
    try {
      final bytes=off?List<int>.filled(widget.count*3,0):pixelProbe(widget.count,index,rgb);
      final hex=bytes.map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
      await widget.client.run('strip frame $hex');
      if(mounted&&widget.client.current)setState(()=>receipt=off?'All-off frame acknowledged':'Pixel ${index+1} frame acknowledged');
    }catch(e){if(mounted)setState(()=>receipt='$e');}
    finally{if(mounted)setState(()=>pending=false);}
  }
  @override Widget build(BuildContext context){
    if(!widget.client.current||!widget.client.session.boardReady||armedOwner!=null&&armedOwner!=widget.client.session.targetStamp)disarm();
    return Column(crossAxisAlignment:CrossAxisAlignment.start,children:[
      Text('Pixel diagnostics',style:Theme.of(context).textTheme.titleMedium),
      Wrap(spacing:4,runSpacing:4,children:[for(var i=0;i<widget.count;i++)ChoiceChip(label:Text('${i+1}'),selected:index==i,onSelected:pending?null:(_)=>setState(()=>index=i))]),
      Row(children:[IconButton(tooltip:'Previous pixel',onPressed:!pending&&index>0?()=>setState(()=>index--):null,icon:const Icon(Icons.chevron_left)),Text('Pixel ${index+1} / ${widget.count}'),IconButton(tooltip:'Next pixel',onPressed:!pending&&index+1<widget.count?()=>setState(()=>index++):null,icon:const Icon(Icons.chevron_right))]),
      CheckboxListTile(contentPadding:EdgeInsets.zero,title:const Text('Arm one test'),value:armedOwner!=null,onChanged:ready?(value)=>setState((){disarm();if(value==true){armedOwner=widget.client.session.targetStamp;expiration=Timer(const Duration(seconds:30),(){if(mounted)setState(disarm);});}}):null),
      Wrap(spacing:8,runSpacing:8,children:[
        for(final (name,rgb) in [('Red',[255,0,0]),('Green',[0,255,0]),('Blue',[0,0,255]),('White',[255,255,255])])Tooltip(message:'Light this pixel and clear all others',child:FilledButton.tonal(onPressed:ready&&armedOwner!=null?()=>unawaited(send(rgb)):null,child:Text(name))),
        OutlinedButton(onPressed:ready?()=>unawaited(send([0,0,0],off:true)):null,child:const Text('All off')),
      ]),
      SizedBox(height:6,child:pending?const LinearProgressIndicator():null),
      if(receipt!=null)SelectableText(receipt!),
    ]);
  }
}
